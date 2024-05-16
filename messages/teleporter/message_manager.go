// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/ethclient"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	gasUtils "github.com/ava-labs/teleporter/utils/gas-utils"
	teleporterUtils "github.com/ava-labs/teleporter/utils/teleporter-utils"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	teleporterMessageCacheSize = 100
)

type messageManager struct {
	messageConfig   Config
	protocolAddress common.Address

	// We parse teleporter messages in ShouldSendMessage, cache them to be reused in SendMessage
	// The cache is keyed by the Warp message ID, NOT the Teleporter message ID
	teleporterMessageCache *cache.LRU[ids.ID, *teleportermessenger.TeleporterMessage]
	destinationClients     map[ids.ID]vms.DestinationClient

	logger logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Address,
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

func (m *messageManager) GetMessageRoutingInfo(unsignedMessage *warp.UnsignedMessage) (
	ids.ID,
	common.Address,
	ids.ID,
	common.Address,
	error,
) {
	teleporterMessage, err := m.parseTeleporterMessage(unsignedMessage)
	if err != nil {
		m.logger.Error(
			"Failed to parse teleporter message.",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		return ids.ID{}, common.Address{}, ids.ID{}, common.Address{}, err
	}
	return unsignedMessage.SourceChainID,
		teleporterMessage.OriginSenderAddress,
		teleporterMessage.DestinationBlockchainID,
		teleporterMessage.DestinationAddress,
		nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(unsignedMessage *warp.UnsignedMessage, destinationBlockchainID ids.ID) (bool, error) {
	teleporterMessage, err := m.parseTeleporterMessage(unsignedMessage)
	if err != nil {
		m.logger.Error(
			"Failed get teleporter message.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return false, err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		// This shouldn't occur, since we already check this in Listener.RouteMessage. Return an error in this case.
		return false, fmt.Errorf("relayer not configured to deliver to destination. destinationBlockchainID=%s", destinationBlockchainID.String())
	}

	teleporterMessageID, err := teleporterUtils.CalculateMessageID(
		m.protocolAddress,
		unsignedMessage.SourceChainID,
		destinationBlockchainID,
		teleporterMessage.MessageNonce,
	)
	if err != nil {
		return false, fmt.Errorf("failed to calculate Teleporter message ID: %w", err)
	}

	senderAddress := destinationClient.SenderAddress()
	if !isAllowedRelayer(teleporterMessage.AllowedRelayerAddresses, senderAddress) {
		m.logger.Info(
			"Relayer EOA not allowed to deliver this message.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return false, nil
	}

	// Check if the message has already been delivered to the destination chain
	teleporterMessenger := m.getTeleporterMessenger(destinationClient)
	delivered, err := teleporterMessenger.MessageReceived(&bind.CallOpts{}, teleporterMessageID)
	if err != nil {
		m.logger.Error(
			"Failed to check if message has been delivered to destination chain.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return false, err
	}
	if delivered {
		m.logger.Info(
			"Message already delivered to destination.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return false, nil
	}

	return true, nil
}

// SendMessage extracts the gasLimit and packs the call data to call the receiveCrossChainMessage method of the Teleporter contract,
// and dispatches transaction construction and broadcast to the destination client
func (m *messageManager) SendMessage(signedMessage *warp.Message, destinationBlockchainID ids.ID) error {
	teleporterMessage, err := m.parseTeleporterMessage(&signedMessage.UnsignedMessage)
	if err != nil {
		m.logger.Error(
			"Failed get teleporter message.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. DestinationBlockchainID=%s", destinationBlockchainID)
	}

	teleporterMessageID, err := teleporterUtils.CalculateMessageID(
		m.protocolAddress,
		signedMessage.SourceChainID,
		destinationBlockchainID,
		teleporterMessage.MessageNonce,
	)
	if err != nil {
		return fmt.Errorf("failed to calculate Teleporter message ID: %w", err)
	}

	m.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessageID.String()),
	)
	numSigners, err := signedMessage.Signature.NumSigners()
	if err != nil {
		m.logger.Error(
			"Failed to get number of signers",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return err
	}
	gasLimit, err := gasUtils.CalculateReceiveMessageGasLimit(numSigners, teleporterMessage.RequiredGasLimit)
	if err != nil {
		m.logger.Error(
			"Gas limit required overflowed uint64 max. not relaying message",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return err
	}
	// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
	callData, err := teleportermessenger.PackReceiveCrossChainMessage(0, common.HexToAddress(m.messageConfig.RewardAddress))
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return err
	}

	err = destinationClient.SendTx(signedMessage, m.protocolAddress.Hex(), gasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return err
	}
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessageID.String()),
	)
	return nil
}

// parseTeleporterMessage returns the Warp message's corresponding Teleporter message from the cache if it exists.
// Otherwise parses the Warp message payload.
func (m *messageManager) parseTeleporterMessage(unsignedMessage *warp.UnsignedMessage) (*teleportermessenger.TeleporterMessage, error) {
	// Check if the message has already been parsed
	teleporterMessage, ok := m.teleporterMessageCache.Get(unsignedMessage.ID())
	if !ok {
		// If not, parse the message
		m.logger.Debug(
			"Teleporter message to send not in cache. Extracting from signed warp message.",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		addressedPayload, err := warpPayload.ParseAddressedCall(unsignedMessage.Payload)
		if err != nil {
			m.logger.Error(
				"Failed parsing addressed payload",
				zap.Error(err),
			)
			return nil, err
		}
		teleporterMessage, err = teleportermessenger.UnpackTeleporterMessage(addressedPayload.Payload)
		if err != nil {
			m.logger.Error(
				"Failed unpacking teleporter message.",
				zap.String("warpMessageID", unsignedMessage.ID().String()),
			)
			return nil, err
		}
		m.teleporterMessageCache.Put(unsignedMessage.ID(), teleporterMessage)
	}
	return teleporterMessage, nil
}

// getTeleporterMessenger returns the Teleporter messenger instance for the destination chain.
// Panic instead of returning errors because this should never happen, and if it does, we do not
// want to log and swallow the error, since operations after this will fail too.
func (m *messageManager) getTeleporterMessenger(destinationClient vms.DestinationClient) *teleportermessenger.TeleporterMessenger {
	client, ok := destinationClient.Client().(ethclient.Client)
	if !ok {
		panic(fmt.Sprintf("Destination client for chain %s is not an Ethereum client", destinationClient.DestinationBlockchainID().String()))
	}

	// Get the teleporter messenger contract
	teleporterMessenger, err := teleportermessenger.NewTeleporterMessenger(m.protocolAddress, client)
	if err != nil {
		panic("Failed to get teleporter messenger contract")
	}
	return teleporterMessenger
}
