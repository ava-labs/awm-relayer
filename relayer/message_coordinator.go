// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type MessageCoordinator struct {
	logger logging.Logger
	// Maps Source blockchain ID and protocol address to a Message Handler Factory
	MessageHandlerFactories map[ids.ID]map[common.Address]messages.MessageHandlerFactory
	ApplicationRelayers     map[common.Hash]*ApplicationRelayer
	SourceClients           map[ids.ID]ethclient.Client
}

func NewMessageCoordinator(
	logger logging.Logger,
	messageHandlerFactories map[ids.ID]map[common.Address]messages.MessageHandlerFactory,
	applicationRelayers map[common.Hash]*ApplicationRelayer,
	sourceClients map[ids.ID]ethclient.Client,
) *MessageCoordinator {
	return &MessageCoordinator{
		logger:                  logger,
		MessageHandlerFactories: messageHandlerFactories,
		ApplicationRelayers:     applicationRelayers,
		SourceClients:           sourceClients,
	}
}

// GetAppRelayerMessageHandler Returns the ApplicationRelayer that is configured to handle this message, as well as a
// one-time MessageHandler instance that the ApplicationRelayer uses to relay this specific message.
// The MessageHandler and ApplicationRelayer are decoupled to support batch workflows in which a single ApplicationRelayer
// processes multiple messages (using their corresponding MessageHandlers) in a single shot.
func (mc *MessageCoordinator) GetAppRelayerMessageHandler(
	warpMessageInfo *relayerTypes.WarpMessageInfo,
) (
	*ApplicationRelayer,
	messages.MessageHandler,
	error,
) {
	// Check that the warp message is from a supported message protocol contract address.
	messageHandlerFactory, supportedMessageProtocol := mc.MessageHandlerFactories[warpMessageInfo.UnsignedMessage.SourceChainID][warpMessageInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single listener instance.
		mc.logger.Debug(
			"Warp message from unsupported message protocol address. Not relaying.",
			zap.String("protocolAddress", warpMessageInfo.SourceAddress.Hex()),
		)
		return nil, nil, nil
	}
	messageHandler, err := messageHandlerFactory.NewMessageHandler(warpMessageInfo.UnsignedMessage)
	if err != nil {
		mc.logger.Error("Failed to create message handler", zap.Error(err))
		return nil, nil, err
	}

	// Fetch the message delivery data
	sourceBlockchainID, originSenderAddress, destinationBlockchainID, destinationAddress, err := messageHandler.GetMessageRoutingInfo()
	if err != nil {
		mc.logger.Error("Failed to get message routing information", zap.Error(err))
		return nil, nil, err
	}

	mc.logger.Info(
		"Unpacked warp message",
		zap.String("sourceBlockchainID", sourceBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("destinationAddress", destinationAddress.String()),
		zap.String("warpMessageID", warpMessageInfo.UnsignedMessage.ID().String()),
	)

	appRelayer := mc.getApplicationRelayer(
		sourceBlockchainID,
		originSenderAddress,
		destinationBlockchainID,
		destinationAddress,
	)
	if appRelayer == nil {
		return nil, nil, nil
	}
	return appRelayer, messageHandler, nil
}

// Unpacks the Warp message and fetches the appropriate application relayer
// Checks for the following registered keys. At most one of these keys should be registered.
// 1. An exact match on sourceBlockchainID, destinationBlockchainID, originSenderAddress, and destinationAddress
// 2. A match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
// 3. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
// 4. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
func (mc *MessageCoordinator) getApplicationRelayer(
	sourceBlockchainID ids.ID,
	originSenderAddress common.Address,
	destinationBlockchainID ids.ID,
	destinationAddress common.Address,
) *ApplicationRelayer {
	// Check for an exact match
	applicationRelayerID := database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		destinationAddress,
	)
	if applicationRelayer, ok := mc.ApplicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := mc.ApplicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		destinationAddress,
	)
	if applicationRelayer, ok := mc.ApplicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := mc.ApplicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}
	mc.logger.Debug(
		"Application relayer not found. Skipping message relay.",
		zap.String("blockchainID", sourceBlockchainID.String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationAddress", destinationAddress.String()),
	)
	return nil
}

func (mc *MessageCoordinator) processManualWarpMessage(
	warpMessage *relayerTypes.WarpMessageInfo,
) (common.Hash, error) {
	// Send any messages that were specified in the configuration
	mc.logger.Info(
		"Relaying manual Warp message",
		zap.String("blockchainID", warpMessage.UnsignedMessage.SourceChainID.String()),
		zap.String("warpMessageID", warpMessage.UnsignedMessage.ID().String()),
	)
	appRelayer, handler, err := mc.GetAppRelayerMessageHandler(warpMessage)
	if err != nil {
		mc.logger.Error(
			"Failed to parse manual Warp message.",
			zap.Error(err),
			zap.String("warpMessageID", warpMessage.UnsignedMessage.ID().String()),
		)
		return common.Hash{}, err
	}

	return appRelayer.ProcessMessage(handler)
}

func (mc *MessageCoordinator) processMessage(blockchainID ids.ID, messageID common.Hash, blockNum *big.Int) (common.Hash, error) {
	ethClient, ok := mc.SourceClients[blockchainID]
	if !ok {
		mc.logger.Error("Source client not found", zap.String("blockchainID", blockchainID.String()))
		return common.Hash{}, fmt.Errorf("source client not set for blockchain: %s", blockchainID.String())
	}

	warpMessage, err := relayerTypes.FetchWarpMessageFromID(ethClient, messageID, blockNum)
	if err != nil {
		mc.logger.Error("Failed to fetch warp from blockchain", zap.String("blockchainID", blockchainID.String()), zap.Error(err))
		return common.Hash{}, fmt.Errorf("could not fetch warp message from ID: %w", err)
	}

	appRelayer, handler, err := mc.GetAppRelayerMessageHandler(warpMessage)
	if err != nil {
		mc.logger.Error(
			"Failed to parse message",
			zap.String("blockchainID", warpMessage.UnsignedMessage.SourceChainID.String()),
			zap.Error(err),
		)
		return common.Hash{}, fmt.Errorf("error getting application relayer: %w", err)
	}
	if appRelayer == nil {
		mc.logger.Error("Application relayer not found")
		return common.Hash{}, errors.New("application relayer not found")
	}

	return appRelayer.ProcessMessage(handler)
}

// Meant to be ran asynchronously. Errors should be sent to errChan.
func (mc *MessageCoordinator) processBlock(blockHeader *types.Header, ethClient ethclient.Client, errChan chan error) {
	// Parse the logs in the block, and group by application relayer
	block, err := relayerTypes.NewWarpBlockInfo(blockHeader, ethClient)
	if err != nil {
		mc.logger.Error("Failed to create Warp block info", zap.Error(err))
		errChan <- err
		return
	}

	// Register each message in the block with the appropriate application relayer
	messageHandlers := make(map[common.Hash][]messages.MessageHandler)
	for _, warpLogInfo := range block.Messages {
		appRelayer, handler, err := mc.GetAppRelayerMessageHandler(warpLogInfo)
		if err != nil {
			mc.logger.Error(
				"Failed to parse message",
				zap.String("blockchainID", warpLogInfo.UnsignedMessage.SourceChainID.String()),
				zap.String("protocolAddress", warpLogInfo.SourceAddress.String()),
				zap.Error(err),
			)
			continue
		}
		if appRelayer == nil {
			mc.logger.Debug("Application relayer not found. Skipping message relay")
			continue
		}
		messageHandlers[appRelayer.relayerID.ID] = append(messageHandlers[appRelayer.relayerID.ID], handler)
	}
	// Initiate message relay of all registered messages
	for _, appRelayer := range mc.ApplicationRelayers {
		// Dispatch all messages in the block to the appropriate application relayer.
		// An empty slice is still a valid argument to ProcessHeight; in this case the height is immediately committed.
		handlers := messageHandlers[appRelayer.relayerID.ID]

		go appRelayer.ProcessHeight(block.BlockNumber, handlers, errChan)
	}
}
