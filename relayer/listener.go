// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/ethclient"
	"github.com/ava-labs/awm-relayer/messages"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	vms "github.com/ava-labs/awm-relayer/vms"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

const (
	maxSubscribeAttempts = 10
	// TODO attempt to resubscribe in perpetuity once we are able to process missed blocks and
	// refresh the chain config on reconnect.
	maxResubscribeAttempts = 10
)

// Listener handles all messages sent from a given source chain
type Listener struct {
	Subscriber              vms.Subscriber
	requestIDLock           *sync.Mutex
	currentRequestID        uint32
	contractMessage         vms.ContractMessage
	messageHandlerFactories map[common.Address]messages.MessageHandlerFactory
	logger                  logging.Logger
	sourceBlockchain        config.SourceBlockchain
	catchUpResultChan       chan bool
	healthStatus            *atomic.Bool
	globalConfig            *config.Config
	applicationRelayers     map[common.Hash]*ApplicationRelayer
	ethClient               ethclient.Client
}

func NewListener(
	logger logging.Logger,
	sourceBlockchain config.SourceBlockchain,
	relayerHealth *atomic.Bool,
	globalConfig *config.Config,
	applicationRelayers map[common.Hash]*ApplicationRelayer,
	startingHeight uint64,
	ethClient ethclient.Client,
) (*Listener, error) {
	blockchainID, err := ids.FromString(sourceBlockchain.BlockchainID)
	if err != nil {
		logger.Error(
			"Invalid blockchainID provided to subscriber",
			zap.Error(err),
		)
		return nil, err
	}
	ethWSClient, err := ethclient.DialWithConfig(
		context.Background(),
		sourceBlockchain.WSEndpoint.BaseURL,
		sourceBlockchain.WSEndpoint.HTTPHeaders,
		sourceBlockchain.WSEndpoint.QueryParams,
	)
	if err != nil {
		logger.Error(
			"Failed to connect to node via WS",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	sub := vms.NewSubscriber(logger, config.ParseVM(sourceBlockchain.VM), blockchainID, ethWSClient)

	// Create message managers for each supported message protocol
	messageHandlerFactories := make(map[common.Address]messages.MessageHandlerFactory)
	for addressStr, cfg := range sourceBlockchain.MessageContracts {
		address := common.HexToAddress(addressStr)
		format := cfg.MessageFormat
		var (
			m   messages.MessageHandlerFactory
			err error
		)
		switch config.ParseMessageProtocol(format) {
		case config.TELEPORTER:
			m, err = teleporter.NewMessageHandlerFactory(
				logger,
				address,
				cfg,
			)
		case config.OFF_CHAIN_REGISTRY:
			m, err = offchainregistry.NewMessageHandlerFactory(
				logger,
				cfg,
			)
		default:
			m, err = nil, fmt.Errorf("invalid message format %s", format)
		}
		if err != nil {
			logger.Error(
				"Failed to create message manager",
				zap.Error(err),
			)
			return nil, err
		}
		messageHandlerFactories[address] = m
	}

	// Marks when the listener has finished the catch-up process on startup.
	// Until that time, we do not know the order in which messages are processed,
	// since the catch-up process occurs concurrently with normal message processing
	// via the subscriber's Subscribe method. As a result, we cannot safely write the
	// latest processed block to the database without risking missing a block in a fault
	// scenario.
	catchUpResultChan := make(chan bool, 1)

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", sourceBlockchain.GetSubnetID().String()),
		zap.String("subnetIDHex", sourceBlockchain.GetSubnetID().Hex()),
		zap.String("blockchainID", sourceBlockchain.GetBlockchainID().String()),
		zap.String("blockchainIDHex", sourceBlockchain.GetBlockchainID().Hex()),
	)
	lstnr := Listener{
		Subscriber:              sub,
		requestIDLock:           &sync.Mutex{},
		currentRequestID:        rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		contractMessage:         vms.NewContractMessage(logger, sourceBlockchain),
		messageHandlerFactories: messageHandlerFactories,
		logger:                  logger,
		sourceBlockchain:        sourceBlockchain,
		catchUpResultChan:       catchUpResultChan,
		healthStatus:            relayerHealth,
		globalConfig:            globalConfig,
		applicationRelayers:     applicationRelayers,
		ethClient:               ethClient,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err = lstnr.Subscriber.Subscribe(maxSubscribeAttempts)
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, err
	}

	if lstnr.globalConfig.ProcessMissedBlocks {
		// Process historical blocks in a separate goroutine so that the main processing loop can
		// start processing new blocks as soon as possible. Otherwise, it's possible for
		// ProcessFromHeight to overload the message queue and cause a deadlock.
		go sub.ProcessFromHeight(big.NewInt(0).SetUint64(startingHeight), lstnr.catchUpResultChan)
	} else {
		lstnr.logger.Info(
			"processed-missed-blocks set to false, starting processing from chain head",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		)
		lstnr.catchUpResultChan <- true
	}

	return &lstnr, nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (lstnr *Listener) ProcessLogs(ctx context.Context) error {
	// Error channel for application relayer errors
	errChan := make(chan error)
	for {
		select {
		case err := <-errChan:
			lstnr.healthStatus.Store(false)
			lstnr.logger.Error(
				"Received error from application relayer",
				zap.Error(err),
			)
		case catchUpResult, ok := <-lstnr.catchUpResultChan:
			// As soon as we've received anything on the channel, there are no more values expected.
			// The expected case is that the channel is closed by the subscriber after writing a value to it,
			// but we also defensively handle an unexpected close.
			lstnr.catchUpResultChan = nil

			// Mark the relayer as unhealthy if the catch-up process fails or if the catch-up channel is unexpectedly closed.
			if !ok {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Catch-up channel unexpectedly closed. Exiting listener goroutine.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("catch-up channel unexpectedly closed")
			}
			if !catchUpResult {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Failed to catch up on historical blocks. Exiting listener goroutine.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("failed to catch up on historical blocks")
			}
		case blockHeader := <-lstnr.Subscriber.Headers():
			// Parse the logs in the block, and group by application relayer

			block, err := relayerTypes.NewWarpBlockInfo(blockHeader, lstnr.ethClient)
			if err != nil {
				lstnr.logger.Error(
					"Failed to create Warp block info",
					zap.Error(err),
				)
				continue
			}

			// Relay the messages in the block to the destination chains. Continue on failure.
			lstnr.logger.Debug(
				"Processing block",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Uint64("blockNumber", block.BlockNumber),
			)

			// Register each message in the block with the appropriate application relayer
			messageHandlers := make(map[common.Hash][]messages.MessageHandler)
			for _, warpLogInfo := range block.Messages {
				appRelayer, handler, err := lstnr.GetAppRelayerMessageHandler(warpLogInfo)
				if err != nil {
					lstnr.logger.Error(
						"Failed to parse message",
						zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
						zap.Error(err),
					)
					continue
				}
				if appRelayer == nil {
					lstnr.logger.Debug("Application relayer not found. Skipping message relay")
					continue
				}
				messageHandlers[appRelayer.relayerID.ID] = append(messageHandlers[appRelayer.relayerID.ID], handler)
			}
			// Initiate message relay of all registered messages
			for _, appRelayer := range lstnr.applicationRelayers {
				// Dispatch all messages in the block to the appropriate application relayer.
				// An empty slice is still a valid argument to ProcessHeight; in this case the height is immediately committed.
				handlers := messageHandlers[appRelayer.relayerID.ID]

				// Process the height async. This is safe because the ApplicationRelayer maintains the threadsafe
				// invariant that heights are committed to the database one at a time, in order, with no gaps.
				go appRelayer.ProcessHeight(block.BlockNumber, handlers, errChan)
			}
		case err := <-lstnr.Subscriber.Err():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Error(
				"Received error from subscribed node",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			// TODO try to resubscribe in perpetuity once we have a mechanism for refreshing state
			// variables such as Quorum values and processing missed blocks.
			err = lstnr.reconnectToSubscriber()
			if err != nil {
				lstnr.logger.Error(
					"Relayer goroutine exiting.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
					zap.Error(err),
				)
				return fmt.Errorf("listener goroutine exiting: %w", err)
			}
		case <-ctx.Done():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Info(
				"Exiting listener because context cancelled",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			)
			return nil
		}
	}
}

// Sets the listener health status to false while attempting to reconnect.
func (lstnr *Listener) reconnectToSubscriber() error {
	// Attempt to reconnect the subscription
	err := lstnr.Subscriber.Subscribe(maxResubscribeAttempts)
	if err != nil {
		return fmt.Errorf("failed to resubscribe to node: %w", err)
	}

	// Success
	lstnr.healthStatus.Store(true)
	return nil
}

// Unpacks the Warp message and fetches the appropriate application relayer
// Checks for the following registered keys. At most one of these keys should be registered.
// 1. An exact match on sourceBlockchainID, destinationBlockchainID, originSenderAddress, and destinationAddress
// 2. A match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
// 3. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
// 4. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
func (lstnr *Listener) getApplicationRelayer(
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
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		destinationAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer
	}
	lstnr.logger.Debug(
		"Application relayer not found. Skipping message relay.",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationAddress", destinationAddress.String()),
	)
	return nil
}

// Returns the ApplicationRelayer that is configured to handle this message, as well as a one-time MessageHandler
// instance that the ApplicationRelayer uses to relay this specific message.
// The MessageHandler and ApplicationRelayer are decoupled to support batch workflows in which a single ApplicationRelayer
// processes multiple messages (using their corresponding MessageHandlers) in a single shot.
func (lstnr *Listener) GetAppRelayerMessageHandler(warpMessageInfo *relayerTypes.WarpMessageInfo) (
	*ApplicationRelayer,
	messages.MessageHandler,
	error,
) {
	// Check that the warp message is from a supported message protocol contract address.
	messageHandlerFactory, supportedMessageProtocol := lstnr.messageHandlerFactories[warpMessageInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single listener instance.
		lstnr.logger.Debug(
			"Warp message from unsupported message protocol address. Not relaying.",
			zap.String("protocolAddress", warpMessageInfo.SourceAddress.Hex()),
		)
		return nil, nil, nil
	}
	messageHandler, err := messageHandlerFactory.NewMessageHandler(warpMessageInfo.UnsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to create message handler",
			zap.Error(err),
		)
		return nil, nil, err
	}

	// Fetch the message delivery data
	sourceBlockchainID, originSenderAddress, destinationBlockchainID, destinationAddress, err := messageHandler.GetMessageRoutingInfo()
	if err != nil {
		lstnr.logger.Error(
			"Failed to get message routing information",
			zap.Error(err),
		)
		return nil, nil, err
	}

	lstnr.logger.Info(
		"Unpacked warp message",
		zap.String("sourceBlockchainID", sourceBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("destinationAddress", destinationAddress.String()),
		zap.String("warpMessageID", warpMessageInfo.UnsignedMessage.ID().String()),
	)

	appRelayer := lstnr.getApplicationRelayer(
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

func (lstnr *Listener) ProcessManualWarpMessages(
	logger logging.Logger,
	manualWarpMessages []*relayerTypes.WarpMessageInfo,
	sourceBlockchain config.SourceBlockchain,
) error {
	// Send any messages that were specified in the configuration
	for _, warpMessage := range manualWarpMessages {
		logger.Info(
			"Relaying manual Warp message",
			zap.String("blockchainID", sourceBlockchain.BlockchainID),
			zap.String("warpMessageID", warpMessage.UnsignedMessage.ID().String()),
		)
		appRelayer, handler, err := lstnr.GetAppRelayerMessageHandler(warpMessage)
		if err != nil {
			logger.Error(
				"Failed to parse manual Warp message.",
				zap.Error(err),
				zap.String("warpMessageID", warpMessage.UnsignedMessage.ID().String()),
			)
			return err
		}
		err = appRelayer.ProcessMessage(handler)
		if err != nil {
			logger.Error(
				"Failed to process manual Warp message",
				zap.String("blockchainID", sourceBlockchain.BlockchainID),
				zap.String("warpMessageID", warpMessage.UnsignedMessage.ID().String()),
			)
			return err
		}
	}
	return nil
}
