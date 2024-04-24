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
	"github.com/ava-labs/avalanchego/vms/platformvm"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
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
	pChainClient        platformvm.Client
	currentRequestID    uint32
	responseChan        chan message.InboundMessage
	contractMessage     vms.ContractMessage
	messageManagers     map[common.Address]messages.MessageManager
	logger              logging.Logger
	sourceBlockchain    config.SourceBlockchain
	catchUpResultChan   chan bool
	healthStatus        *atomic.Bool
	globalConfig        *config.Config
	dbManager           *database.DatabaseManager
	applicationRelayers map[common.Hash]*applicationRelayer
}

func NewListener(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	dbManager *database.DatabaseManager,
	sourceBlockchain config.SourceBlockchain,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
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
	ethClient, err := ethclient.Dial(sourceBlockchain.WSEndpoint)
	if err != nil {
		logger.Error(
			"Failed to connect to node",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	sub := vms.NewSubscriber(logger, config.ParseVM(sourceBlockchain.VM), blockchainID, ethClient)

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

	// Create the application relayers
	applicationRelayers := make(map[common.Hash]*applicationRelayer)
	for _, relayerID := range database.GetSourceBlockchainRelayerIDs(&sourceBlockchain) {
		applicationRelayer, err := newApplicationRelayer(
			logger,
			metrics,
			network,
			messageCreator,
			responseChan,
			relayerID,
			dbManager,
			sourceBlockchain,
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
		pChainClient:        pChainClient,
		currentRequestID:    rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		responseChan:        responseChan,
		contractMessage:     vms.NewContractMessage(logger, sourceBlockchain),
		messageManagers:     messageManagers,
		logger:              logger,
		sourceBlockchain:    sourceBlockchain,
		catchUpResultChan:   catchUpResultChan,
		healthStatus:        relayerHealth,
		globalConfig:        cfg,
		dbManager:           dbManager,
		applicationRelayers: applicationRelayers,
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
		height, err := lstnr.calculateStartingBlockHeight(sourceBlockchain.ProcessHistoricalBlocksFromHeight)
		if err != nil {
			logger.Error(
				"Failed to calculate starting block height on startup",
				zap.Error(err),
			)
			return nil, err
		}
		// Process historical blocks in a separate goroutine so that the main processing loop can
		// start processing new blocks as soon as possible. Otherwise, it's possible for
		// ProcessFromHeight to overload the message queue and cause a deadlock.
		go sub.ProcessFromHeight(big.NewInt(0).SetUint64(height), lstnr.catchUpResultChan)
	} else {
		lstnr.logger.Info(
			"processed-missed-blocks set to false, starting processing from chain head",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		)
		err = lstnr.setAllProcessedBlockHeightsToLatest()
		if err != nil {
			logger.Error(
				"Failed to update latest processed block. Continuing to normal relaying operation",
				zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			return nil, err
		}
		lstnr.catchUpResultChan <- true
	}

	return &lstnr, nil
}

// Gets the height of the chain head.
func (lstnr *Listener) getCurrentHeight() (uint64, error) {
	ethClient, err := ethclient.Dial(lstnr.sourceBlockchain.RPCEndpoint)
	if err != nil {
		lstnr.logger.Error(
			"Failed to dial node",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.Error(err),
		)
		return 0, err
	}

	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		lstnr.logger.Error(
			"Failed to get latest block",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.Error(err),
		)
		return 0, err
	}

	return latestBlock, nil
}

// Calculates the listener's starting block height as the minimum of all of the composed application relayer starting block heights
func (lstnr *Listener) calculateStartingBlockHeight(processHistoricalBlocksFromHeight uint64) (uint64, error) {
	minHeight := uint64(0)
	currentHeight, err := lstnr.getCurrentHeight()
	if err != nil {
		return 0, err
	}
	for _, relayer := range lstnr.applicationRelayers {
		height, err := relayer.calculateStartingBlockHeight(processHistoricalBlocksFromHeight, currentHeight)
		if err != nil {
			return 0, err
		}
		if minHeight == 0 || height < minHeight {
			minHeight = height
		}
	}
	return minHeight, nil
}

func (lstnr *Listener) setAllProcessedBlockHeightsToLatest() error {
	for _, relayer := range lstnr.applicationRelayers {
		height, err := lstnr.getCurrentHeight()
		if err != nil {
			return err
		}
		relayer.dbManager.PrepareHeight(relayer.relayerID, height, 0)
		if err != nil {
			return err
		}
	}
	return nil
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
		SourceTxID:       log.TxHash[:],
		UnsignedMsgBytes: log.Data,
		BlockNumber:      log.BlockNumber,
	}, nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (lstnr *Listener) ProcessLogs(ctx context.Context) error {
	doneCatchingUp := false
	for {
		select {
		case catchUpResult := <-lstnr.catchUpResultChan:
			if !catchUpResult {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Failed to catch up on historical blocks. Exiting listener goroutine.",
					zap.String("originChainId", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("failed to catch up on historical blocks")
			} else {
				doneCatchingUp = true
			}
		case block := <-lstnr.Subscriber.Blocks():
			// Relay the messages in the block to the destination chains. Continue on failure.
			isCheckpoint := doneCatchingUp || block.IsCatchUpBlock
			lstnr.logger.Info(
				"Processing block",
				zap.String("originChainId", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Uint64("blockNumber", block.BlockNumber),
				zap.Bool("isCheckpoint", isCheckpoint),
			)

			// Iterate over the Warp logs in two passes:
			// The first pass extracts the information needed to relay from the log, but does not initiate relaying
			// This is so that the number of messages to be processed can be registered with the database before
			// any messages are processed
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
			if isCheckpoint {
				for _, appRelayer := range lstnr.applicationRelayers {
					// Prepare the each application relayer's database key with the number
					// of expected messages. If no messages are found in the above loop, then
					// totalMessages will be 0
					totalMessages := expectedMessages[appRelayer.relayerID]
					err := lstnr.dbManager.PrepareHeight(appRelayer.relayerID, block.BlockNumber, totalMessages)
					if err != nil {
						lstnr.logger.Error(
							"Failed to prepare height",
							zap.String("relayerID", appRelayer.relayerID.ID.String()),
							zap.Error(err),
						)
						continue
					}
				}
			}
			for _, msgInfo := range msgsInfo {
				err := lstnr.dispatchToApplicationRelayer(msgInfo, block.BlockNumber)
				if err != nil {
					lstnr.logger.Error(
						"Error relaying message",
						zap.String("originChainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
						zap.Error(err),
					)
					continue
				}
			}
		case err := <-lstnr.Subscriber.Err():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Error(
				"Received error from subscribed node",
				zap.String("originChainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			// TODO try to resubscribe in perpetuity once we have a mechanism for refreshing state
			// variables such as Quorum values and processing missed blocks.
			err = lstnr.ReconnectToSubscriber()
			if err != nil {
				lstnr.logger.Error(
					"Relayer goroutine exiting.",
					zap.String("originChainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
					zap.Error(err),
				)
				return fmt.Errorf("listener goroutine exiting: %w", err)
			}
		case <-ctx.Done():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Info(
				"Exiting listener because context cancelled",
				zap.String("originChainId", lstnr.sourceBlockchain.GetBlockchainID().String()),
			)
			return nil
		}
	}
}

// Sets the listener health status to false while attempting to reconnect.
func (lstnr *Listener) ReconnectToSubscriber() error {
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
func (lstnr *Listener) RouteMessage(warpLogInfo *relayerTypes.WarpLogInfo) error {
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

	return lstnr.dispatchToApplicationRelayer(parsedMessageInfo, warpLogInfo.BlockNumber)
}

// Unpacks the Warp message and fetches the appropriate application relayer
// Checks for the following registered keys. At most one of these keys should be registered.
// 1. An exact match on sourceBlockchainID, destinationBlockchainID, originSenderAddress, and destinationAddress
// 2. A match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
// 3. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
// 4. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
func (lstnr *Listener) getApplicationRelayer(
	unsignedMessage *avalancheWarp.UnsignedMessage,
	messageManager messages.MessageManager,
) (*applicationRelayer, bool) {
	// Fetch the message delivery data
	sourceBlockchainID, originSenderAddress, destinationBlockchainID, destinationAddress, err := messageManager.GetMessageRoutingInfo(unsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to get message routing information",
			zap.Error(err),
		)
		return nil, false
	}

	// Check for an exact match
	applicationRelayerID := database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		destinationAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer, ok
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer, ok
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		destinationAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer, ok
	}

	// Check for a match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
	applicationRelayerID = database.CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		database.AllAllowedAddress,
		database.AllAllowedAddress,
	)
	if applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]; ok {
		return applicationRelayer, ok
	}
	lstnr.logger.Debug(
		"Application relayer not found. Skipping message relay.",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("originSenderAddress", originSenderAddress.String()),
		zap.String("destinationAddress", destinationAddress.String()),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
	)
	return nil, false
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

	lstnr.logger.Info(
		"Unpacked warp message",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
	)

	lstnr.logger.Info(
		"Relaying message",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
	)

	applicationRelayer, ok := lstnr.getApplicationRelayer(
		unsignedMessage,
		messageManager,
	)
	if !ok {
		return nil, nil
	}
	return &parsedMessageInfo{
		unsignedMessage:    unsignedMessage,
		messageManager:     messageManager,
		applicationRelayer: applicationRelayer,
	}, nil
}

func (lstnr *Listener) dispatchToApplicationRelayer(parsedMessageInfo *parsedMessageInfo, blockNumber uint64) error {
	// Relay the message to the destination. Messages from a given source chain must be processed in serial in order to
	// guarantee that the previous block (n-1) is fully processed by the listener when processing a given log from block n.
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
