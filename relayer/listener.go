// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	vms "github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
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

var ErrInvalidLog = errors.New("invalid warp message log")

// Listener handles all messages sent from a given source chain
type Listener struct {
	Subscriber          vms.Subscriber
	currentRequestID    uint32
	contractMessage     vms.ContractMessage
	messageManagers     map[common.Address]messages.MessageManager
	logger              logging.Logger
	sourceBlockchain    config.SourceBlockchain
	catchUpResultChan   chan bool
	healthStatus        *atomic.Bool
	globalConfig        *config.Config
	applicationRelayers map[common.Hash]*applicationRelayer
	ethClient           ethclient.Client
}

func NewListener(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	sourceBlockchain config.SourceBlockchain,
	network *peers.AppRequestNetwork,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	relayerHealth *atomic.Bool,
	cfg *config.Config,
) (*Listener, error) {
	blockchainID, err := ids.FromString(sourceBlockchain.BlockchainID)
	if err != nil {
		logger.Error(
			"Invalid blockchainID provided to subscriber",
			zap.Error(err),
		)
		return nil, err
	}
	ethWSClient, err := ethclient.Dial(sourceBlockchain.WSEndpoint)
	if err != nil {
		logger.Error(
			"Failed to connect to node via WS",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	sub := vms.NewSubscriber(logger, config.ParseVM(sourceBlockchain.VM), blockchainID, ethWSClient)

	ethRPCClient, err := ethclient.Dial(sourceBlockchain.RPCEndpoint)
	if err != nil {
		logger.Error(
			"Failed to connect to node via RPC",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	// Create message managers for each supported message protocol
	messageManagers := make(map[common.Address]messages.MessageManager)
	for addressStr, config := range sourceBlockchain.MessageContracts {
		address := common.HexToAddress(addressStr)
		messageManager, err := messages.NewMessageManager(logger, address, config, destinationClients)
		if err != nil {
			logger.Error(
				"Failed to create message manager",
				zap.Error(err),
			)
			return nil, err
		}
		messageManagers[address] = messageManager
	}

	// Marks when the listener has finished the catch-up process on startup.
	// Until that time, we do not know the order in which messages are processed,
	// since the catch-up process occurs concurrently with normal message processing
	// via the subscriber's Subscribe method. As a result, we cannot safely write the
	// latest processed block to the database without risking missing a block in a fault
	// scenario.
	catchUpResultChan := make(chan bool, 1)

	currentHeight, err := ethRPCClient.BlockNumber(context.Background())
	if err != nil {
		logger.Error(
			"Failed to get current block height",
			zap.Error(err),
		)
		return nil, err
	}

	// Create the application relayers
	applicationRelayers := make(map[common.Hash]*applicationRelayer)
	minHeight := uint64(0)

	for _, relayerID := range database.GetSourceBlockchainRelayerIDs(&sourceBlockchain) {
		height, err := database.CalculateStartingBlockHeight(
			logger,
			db,
			relayerID,
			sourceBlockchain.ProcessHistoricalBlocksFromHeight,
			currentHeight,
		)
		if err != nil {
			logger.Error(
				"Failed to calculate starting block height",
				zap.String("relayerID", relayerID.ID.String()),
				zap.Error(err),
			)
			return nil, err
		}
		if minHeight == 0 || height < minHeight {
			minHeight = height
		}
		applicationRelayer, err := newApplicationRelayer(
			logger,
			metrics,
			network,
			messageCreator,
			relayerID,
			db,
			ticker,
			sourceBlockchain,
			height,
			cfg,
		)
		if err != nil {
			logger.Error(
				"Failed to create application relayer",
				zap.String("relayerID", relayerID.ID.String()),
				zap.Error(err),
			)
			return nil, err
		}
		applicationRelayers[relayerID.ID] = applicationRelayer
	}

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", sourceBlockchain.GetSubnetID().String()),
		zap.String("subnetIDHex", sourceBlockchain.GetSubnetID().Hex()),
		zap.String("blockchainID", sourceBlockchain.GetBlockchainID().String()),
		zap.String("blockchainIDHex", sourceBlockchain.GetBlockchainID().Hex()),
	)
	lstnr := Listener{
		Subscriber:          sub,
		currentRequestID:    rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		contractMessage:     vms.NewContractMessage(logger, sourceBlockchain),
		messageManagers:     messageManagers,
		logger:              logger,
		sourceBlockchain:    sourceBlockchain,
		catchUpResultChan:   catchUpResultChan,
		healthStatus:        relayerHealth,
		globalConfig:        cfg,
		applicationRelayers: applicationRelayers,
		ethClient:           ethRPCClient,
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
		go sub.ProcessFromHeight(big.NewInt(0).SetUint64(minHeight), lstnr.catchUpResultChan)
	} else {
		lstnr.logger.Info(
			"processed-missed-blocks set to false, starting processing from chain head",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		)
		lstnr.catchUpResultChan <- true
	}

	return &lstnr, nil
}

func (lstnr *Listener) NewWarpLogInfo(log types.Log) (*relayerTypes.WarpLogInfo, error) {
	if len(log.Topics) != 3 {
		lstnr.logger.Error(
			"Log did not have the correct number of topics",
			zap.Int("numTopics", len(log.Topics)),
		)
		return nil, ErrInvalidLog
	}
	if log.Topics[0] != warp.WarpABI.Events["SendWarpMessage"].ID {
		lstnr.logger.Error(
			"Log topic does not match the SendWarpMessage event type",
			zap.String("topic", log.Topics[0].String()),
			zap.String("expectedTopic", warp.WarpABI.Events["SendWarpMessage"].ID.String()),
		)
		return nil, ErrInvalidLog
	}

	return &relayerTypes.WarpLogInfo{
		// BytesToAddress takes the last 20 bytes of the byte array if it is longer than 20 bytes
		SourceAddress:    common.BytesToAddress(log.Topics[1][:]),
		UnsignedMsgBytes: log.Data,
	}, nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (lstnr *Listener) ProcessLogs(ctx context.Context) error {
	for {
		select {
		case catchUpResult := <-lstnr.catchUpResultChan:
			if !catchUpResult {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Failed to catch up on historical blocks. Exiting listener goroutine.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("failed to catch up on historical blocks")
			}
		case block := <-lstnr.Subscriber.Blocks():
			// Relay the messages in the block to the destination chains. Continue on failure.
			lstnr.logger.Info(
				"Processing block",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Uint64("blockNumber", block.BlockNumber),
			)

			// Iterate over the Warp logs in two passes:
			// The first pass extracts the information needed to relay from the log, but does not initiate relaying
			// This is so that the number of messages to be processed can be registered with the database before
			// any messages are processed.
			// The second pass dispatches the messages to the application relayers for processing
			expectedMessages := make(map[database.RelayerID]uint64)
			var msgsInfo []*parsedMessageInfo
			for _, warpLog := range block.WarpLogs {
				warpLogInfo, err := lstnr.NewWarpLogInfo(warpLog)
				if err != nil {
					lstnr.logger.Error(
						"Failed to create warp log info",
						zap.Error(err),
					)
					continue
				}
				msgInfo, err := lstnr.parseMessage(warpLogInfo)
				if err != nil {
					lstnr.logger.Error(
						"Failed to parse message",
						zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
						zap.Error(err),
					)
					continue
				}
				if msgInfo == nil {
					lstnr.logger.Debug(
						"Application relayer not found. Skipping message relay.",
						zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
					)
					continue
				}
				msgsInfo = append(msgsInfo, msgInfo)
				expectedMessages[msgInfo.applicationRelayer.relayerID]++
			}
			for _, appRelayer := range lstnr.applicationRelayers {
				// Prepare the each application relayer's database key with the number
				// of expected messages. If no messages are found in the above loop, then
				// totalMessages will be 0
				totalMessages := expectedMessages[appRelayer.relayerID]
				appRelayer.checkpointManager.PrepareHeight(block.BlockNumber, totalMessages)
			}
			for _, msgInfo := range msgsInfo {
				lstnr.logger.Info(
					"Relaying message",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
					zap.String("warpMessageID", msgInfo.unsignedMessage.ID().String()),
				)
				err := lstnr.dispatchToApplicationRelayer(msgInfo, block.BlockNumber)
				if err != nil {
					lstnr.logger.Error(
						"Error relaying message",
						zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
						zap.Error(err),
					)
					continue
				}
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

// RouteMessage relays a single warp message to the destination chain.
// Warp message relay requests from the same origin chain are processed serially
func (lstnr *Listener) RouteManualWarpMessage(warpLogInfo *relayerTypes.WarpLogInfo) error {
	parsedMessageInfo, err := lstnr.parseMessage(warpLogInfo)
	if err != nil {
		lstnr.logger.Error(
			"Failed to parse message",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.Error(err),
		)
		return err
	}
	if parsedMessageInfo == nil {
		lstnr.logger.Debug(
			"Application relayer not found. Skipping message relay.",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		)
		return nil
	}

	return lstnr.dispatchToApplicationRelayer(parsedMessageInfo, 0)
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
	messageManager messages.MessageManager,
) *applicationRelayer {
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

// Helper type and function to extract the information needed to relay a message
// From the raw log information
type parsedMessageInfo struct {
	unsignedMessage    *avalancheWarp.UnsignedMessage
	messageManager     messages.MessageManager
	applicationRelayer *applicationRelayer
}

func (lstnr *Listener) parseMessage(warpLogInfo *relayerTypes.WarpLogInfo) (
	*parsedMessageInfo,
	error,
) {
	// Check that the warp message is from a supported message protocol contract address.
	messageManager, supportedMessageProtocol := lstnr.messageManagers[warpLogInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single listener instance.
		lstnr.logger.Debug(
			"Warp message from unsupported message protocol address. Not relaying.",
			zap.String("protocolAddress", warpLogInfo.SourceAddress.Hex()),
		)
		return nil, nil
	}

	// Unpack the VM message bytes into a Warp message
	unsignedMessage, err := lstnr.contractMessage.UnpackWarpMessage(warpLogInfo.UnsignedMsgBytes)
	if err != nil {
		lstnr.logger.Error(
			"Failed to unpack sender message",
			zap.Error(err),
		)
		return nil, err
	}
	// Fetch the message delivery data
	sourceBlockchainID, originSenderAddress, destinationBlockchainID, destinationAddress, err := messageManager.GetMessageRoutingInfo(unsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to get message routing information",
			zap.Error(err),
		)
		return nil, err
	}

	lstnr.logger.Info(
		"Unpacked warp message",
		zap.String("sourceBlockchainID", sourceBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("destinationAddress", destinationAddress.String()),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
	)

	applicationRelayer := lstnr.getApplicationRelayer(
		sourceBlockchainID,
		originSenderAddress,
		destinationBlockchainID,
		destinationAddress,
		messageManager,
	)
	if applicationRelayer == nil {
		return nil, nil
	}
	return &parsedMessageInfo{
		unsignedMessage:    unsignedMessage,
		messageManager:     messageManager,
		applicationRelayer: applicationRelayer,
	}, nil
}

func (lstnr *Listener) dispatchToApplicationRelayer(parsedMessageInfo *parsedMessageInfo, blockNumber uint64) error {
	// TODO: Add a config option to use the Warp API, instead of hardcoding to the app request network here
	err := parsedMessageInfo.applicationRelayer.relayMessage(
		parsedMessageInfo.unsignedMessage,
		lstnr.currentRequestID,
		parsedMessageInfo.messageManager,
		blockNumber,
		true,
	)
	if err != nil {
		lstnr.logger.Error(
			"Failed to run application relayer",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.String("warpMessageID", parsedMessageInfo.unsignedMessage.ID().String()),
			zap.Error(err),
		)
	}

	// Increment the request ID for the next message relay request
	lstnr.currentRequestID++
	return err
}
