// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	gasUtils "github.com/ava-labs/teleporter/utils/gas-utils"
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
	teleporterMessageCache *cache.LRU[ids.ID, *teleportermessenger.TeleporterMessage]
	destinationClients     map[ids.ID]vms.DestinationClient

	logger logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
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
	teleporterMessageCache := &cache.LRU[ids.ID, *teleportermessenger.TeleporterMessage]{Size: teleporterMessageCacheSize}

	return &messageManager{
		messageConfig:          messageConfig,
		protocolAddress:        messageProtocolAddress,
		teleporterMessageCache: teleporterMessageCache,
		destinationClients:     destinationClients,
		logger:                 logger,
	}, nil
}

func isAllowedRelayer(allowedRelayers []common.Address, eoa common.Address) bool {
	// If no allowed relayer addresses were set, then anyone can relay it.
	if len(allowedRelayers) == 0 {
		return true
	}

	for _, addr := range allowedRelayers {
		if addr == eoa {
			return true
		}
	}
	return false
}

func (m *messageManager) GetDestinationBlockchainID(warpMessageInfo *vmtypes.WarpMessageInfo) (ids.ID, error) {
	teleporterMessage, err := m.parseTeleporterMessage(warpMessageInfo.WarpUnsignedMessage.ID(), warpMessageInfo.WarpPayload)
	if err != nil {
		m.logger.Error(
			"Failed get teleporter message.",
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
		)
		return ids.ID{}, err
	}

	// Cache the message so it can be reused in SendMessage
	m.teleporterMessageCache.Put(warpMessageInfo.WarpUnsignedMessage.ID(), teleporterMessage)

	return teleporterMessage.DestinationBlockchainID, nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationBlockchainID ids.ID) (bool, error) {
	teleporterMessage, err := m.parseTeleporterMessage(warpMessageInfo.WarpUnsignedMessage.ID(), warpMessageInfo.WarpPayload)
	if err != nil {
		m.logger.Error(
			"Failed get teleporter message.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
			zap.Error(err),
		)
		return false, err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return false, fmt.Errorf("relayer not configured to deliver to destination. DestinationBlockchainID=%s", destinationBlockchainID.String())
	}

	senderAddress := destinationClient.SenderAddress()
	if !isAllowedRelayer(teleporterMessage.AllowedRelayerAddresses, senderAddress) {
		m.logger.Info(
			"Relayer EOA not allowed to deliver this message.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
		)
		return false, nil
	}

	delivered, err := m.messageDelivered(
		destinationClient,
		warpMessageInfo,
		teleporterMessage,
		destinationBlockchainID,
	)
	if err != nil {
		m.logger.Error(
			"Failed to check if message has been delivered to destination chain.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
			zap.Error(err),
		)
		return false, err
	}
	if delivered {
		m.logger.Info(
			"Message already delivered to destination.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
		)
		return false, nil
	}

	// Cache the message so it can be reused in SendMessage
	m.teleporterMessageCache.Put(warpMessageInfo.WarpUnsignedMessage.ID(), teleporterMessage)
	return true, nil
}

// Helper to check if a message has been delivered to the destination chain
// Returns true if the message has been delivered, false if not
// On error, the boolean result should be ignored
func (m *messageManager) messageDelivered(
	destinationClient vms.DestinationClient,
	warpMessageInfo *vmtypes.WarpMessageInfo,
	teleporterMessage *teleportermessenger.TeleporterMessage,
	destinationBlockchainID ids.ID) (bool, error) {
	// Check if the message has already been delivered to the destination chain
	client, ok := destinationClient.Client().(ethclient.Client)
	if !ok {
		m.logger.Error(
			"Destination client is not an Ethereum client.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
		)
		return false, errors.New("destination client is not an Ethereum client")
	}

	data, err := teleportermessenger.PackMessageReceived(
		warpMessageInfo.WarpUnsignedMessage.SourceChainID,
		teleporterMessage.MessageID,
	)
	if err != nil {
		m.logger.Error(
			"Failed packing messageReceived call data.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.Error(err),
		)
		return false, err
	}
	protocolAddress := common.BytesToAddress(m.protocolAddress[:])
	callMessage := interfaces.CallMsg{
		To:   &protocolAddress,
		Data: data,
	}
	result, err := client.CallContract(context.Background(), callMessage, nil)
	if err != nil {
		m.logger.Error(
			"Failed calling messageReceived method on destination chain.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.Error(err),
		)
		return false, err
	}
	// check the contract call result
	delivered, err := teleportermessenger.UnpackMessageReceivedResult(result)
	if err != nil {
		m.logger.Error(
			"Failed unpacking messageReceived result.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.Error(err),
		)
		return false, err
	}

	return delivered, nil
}

// SendMessage extracts the gasLimit and packs the call data to call the receiveCrossChainMessage method of the Teleporter contract,
// and dispatches transaction construction and broadcast to the destination client
func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationBlockchainID ids.ID) error {
	teleporterMessage, err := m.parseTeleporterMessage(signedMessage.ID(), parsedVmPayload)
	if err != nil {
		m.logger.Error(
			"Failed get teleporter message.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}

	m.logger.Info(
		"Sending message to destination chain",
		zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
	)
	numSigners, err := signedMessage.Signature.NumSigners()
	if err != nil {
		m.logger.Error(
			"Failed to get number of signers",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
		)
		return err
	}
	gasLimit, err := gasUtils.CalculateReceiveMessageGasLimit(numSigners, teleporterMessage.RequiredGasLimit)
	if err != nil {
		m.logger.Error(
			"Gas limit required overflowed uint64 max. not relaying message",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
		)
		return err
	}
	// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
	callData, err := teleportermessenger.PackReceiveCrossChainMessage(0, common.HexToAddress(m.messageConfig.RewardAddress))
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
		)
		return err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. DestinationBlockchainID=%s", destinationBlockchainID)
	}
	err = destinationClient.SendTx(signedMessage, m.protocolAddress.Hex(), gasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
			zap.Error(err),
		)
		return err
	}
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("DestinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessage.MessageID.String()),
	)
	return nil
}

// parseTeleporterMessage returns the Warp message's corresponding Teleporter message from the cache if it exists.
// Otherwise parses the Warp message payload.
func (m *messageManager) parseTeleporterMessage(warpMessageID ids.ID, warpPayload []byte) (*teleportermessenger.TeleporterMessage, error) {
	// Check if the message has already been parsed
	teleporterMessage, ok := m.teleporterMessageCache.Get(warpMessageID)
	if !ok {
		// If not, parse the message
		m.logger.Debug(
			"Teleporter message to send not in cache. Extracting from signed warp message.",
			zap.String("warpMessageID", warpMessageID.String()),
		)
		var err error
		teleporterMessage, err = teleportermessenger.UnpackTeleporterMessage(warpPayload)
		if err != nil {
			m.logger.Error(
				"Failed unpacking teleporter message.",
				zap.String("warpMessageID", warpMessageID.String()),
			)
			return nil, err
		}
	}
	return teleporterMessage, nil
}
