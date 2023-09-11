// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	teleporterMessageCacheSize = 100
)

type messageManager struct {
	messageConfig   Config
	protocolAddress common.Hash

	// We parse teleporter messages in ShouldSendMessage, cache them to be reused in SendMessage
	// The cache is keyed by the Warp message ID, NOT the Teleporter message ID
	teleporterMessageCache *cache.LRU[ids.ID, *TeleporterMessage]
	sourceClient           ethclient.Client
	supportedFeeAssets     set.Set[common.Address]
	destinationClients     map[ids.ID]vms.DestinationClient

	logger logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
	sourceSubnetInfo config.SourceSubnet,
) (*messageManager, error) {
	// Marshal the map and unmarshal into the Teleporter config
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		logger.Error("Failed to marshal Teleporter config")
		return nil, err
	}
	var messageConfig Config
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		logger.Error("Failed to unmarshal Teleporter config")
		return nil, err
	}

	if err := messageConfig.Validate(); err != nil {
		logger.Error(
			"Invalid Teleporter config.",
			zap.Error(err),
		)
		return nil, err
	}
	teleporterMessageCache := &cache.LRU[ids.ID, *TeleporterMessage]{Size: teleporterMessageCacheSize}

	// If we are restricting the fee assets, dial the source client
	var client ethclient.Client
	if len(messageConfig.FeeAssets) != 0 {
		client, err = ethclient.Dial(sourceSubnetInfo.GetNodeRPCEndpoint())
		if err != nil {
			logger.Error(
				"Failed to dial source client rpc endpoint",
				zap.Error(err),
			)
			return nil, err
		}
	}

	supportedFeeAssetsSet := set.NewSet[common.Address](len(messageConfig.FeeAssets))
	for _, asset := range messageConfig.FeeAssets {
		supportedFeeAssetsSet.Add(common.HexToAddress(asset))
	}

	return &messageManager{
		messageConfig:          messageConfig,
		protocolAddress:        messageProtocolAddress,
		teleporterMessageCache: teleporterMessageCache,
		destinationClients:     destinationClients,
		sourceClient:           client,
		supportedFeeAssets:     supportedFeeAssetsSet,
		logger:                 logger,
	}, nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationChainID ids.ID) (bool, error) {
	// Unpack the teleporter message and add it to the cache
	teleporterMessage, err := unpackTeleporterMessage(warpMessageInfo.WarpPayload)
	if err != nil {
		m.logger.Error(
			"Failed unpacking teleporter message.",
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
		)
		return false, err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationChainID]
	if !ok {
		return false, fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID.String())
	}
	if !destinationClient.Allowed(destinationChainID, teleporterMessage.AllowedRelayerAddresses) {
		m.logger.Info(
			"Relayer not allowed to deliver to chain.",
			zap.String("destinationChainID", destinationChainID.String()),
		)
		return false, nil
	}

	// Check if the relayer fee is in a supported asset.
	// If the configured list of fee assets is empty, all assets are supported.
	if m.supportedFeeAssets.Len() != 0 {
		teleporterAddress := common.BytesToAddress(m.protocolAddress.Bytes())
		callData, err := EVMTeleporterContractABI.Pack(
			"getFeeInfo",
			destinationChainID,
			teleporterMessage.MessageID,
		)
		if err != nil {
			m.logger.Error(
				"Failed to pack getFeeInfo call",
				zap.Error(err),
			)
			return false, err
		}
		getFeeInfoCallMsg := interfaces.CallMsg{
			To:   &teleporterAddress,
			Data: callData,
		}
		res, err := m.sourceClient.CallContract(context.Background(), getFeeInfoCallMsg, nil)
		if err != nil {
			m.logger.Error(
				"Failed to call getFeeInfo",
				zap.Error(err),
			)
			return false, err
		}
		var feeInfo GetFeeInfoOutput
		err = EVMTeleporterContractABI.UnpackIntoInterface(&feeInfo, "getFeeInfo", res)
		if err != nil {
			m.logger.Error(
				"Failed to unpack getFeeInfo output",
				zap.Error(err),
			)
			return false, err
		}

		if !m.supportedFeeAssets.Contains(feeInfo.FeeAsset) {
			m.logger.Info(
				"Relayer fee asset not supported.",
				zap.String("feeAsset", feeInfo.FeeAsset.String()),
			)
			return false, nil
		}
	}

	// Cache the message so it can be reused in SendMessage
	m.teleporterMessageCache.Put(warpMessageInfo.WarpUnsignedMessage.ID(), teleporterMessage)
	return true, nil
}

// SendMessage extracts the gasLimit and packs the call data to call the receiveCrossChainMessage method of the Teleporter contract,
// and dispatches transaction construction and broadcast to the destination client
func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationChainID ids.ID) error {
	var (
		teleporterMessage *TeleporterMessage
		ok                bool
	)
	teleporterMessage, ok = m.teleporterMessageCache.Get(signedMessage.ID())
	if !ok {
		m.logger.Debug(
			"Teleporter message to send not in cache. Extracting from signed warp message.",
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		var err error
		teleporterMessage, err = unpackTeleporterMessage(parsedVmPayload)
		if err != nil {
			m.logger.Error(
				"Failed unpacking teleporter message.",
				zap.String("warpMessageID", signedMessage.ID().String()),
			)
			return err
		}
	}

	m.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationChainID", destinationChainID.String()),
		zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
	)
	numSigners, err := signedMessage.Signature.NumSigners()
	if err != nil {
		m.logger.Error(
			"Failed to get number of signers",
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}
	gasLimit, err := CalculateReceiveMessageGasLimit(numSigners, teleporterMessage.RequiredGasLimit)
	if err != nil {
		m.logger.Error(
			"Gas limit required overflowed uint64 max. not relaying message",
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}
	// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
	callData, err := packReceiverMessage(ReceiveCrossChainMessageInput{
		RelayerRewardAddress: common.HexToAddress(m.messageConfig.RewardAddress),
	})
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationChainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID)
	}
	err = destinationClient.SendTx(signedMessage, m.protocolAddress.Hex(), gasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("destinationChainID", destinationChainID.String()),
			zap.Error(err),
		)
		return err
	}
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationChainID", destinationChainID.String()),
		zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
	)
	return nil
}
