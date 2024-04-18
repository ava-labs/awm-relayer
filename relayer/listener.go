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
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	vms "github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/core/types"
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
	applicationRelayers map[common.Hash]*applicationRelayer
}

func NewListener(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	db database.RelayerDatabase,
	sourceBlockchain config.SourceBlockchain,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	relayerHealth *atomic.Bool,
	cfg *config.Config,
) (*Listener, error) {
	sub := vms.NewSubscriber(logger, sourceBlockchain)

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
			db,
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
		applicationRelayers: applicationRelayers,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err := lstnr.Subscriber.Subscribe(maxSubscribeAttempts)
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

// Calculates the listener's starting block height as the minimum of all of the composed application relayer starting block heights
func (lstnr *Listener) calculateStartingBlockHeight(processHistoricalBlocksFromHeight uint64) (uint64, error) {
	minHeight := uint64(0)
	for _, relayer := range lstnr.applicationRelayers {
		height, err := relayer.calculateStartingBlockHeight(processHistoricalBlocksFromHeight)
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
		_, err := relayer.setProcessedBlockHeightToLatest()
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
	sourceAddress := common.BytesToAddress(log.Topics[1][:])
	unsignedMessageBytes := log.Data

	// Unpack the VM message bytes into a Warp message
	unsignedMessage, err := lstnr.contractMessage.UnpackWarpMessage(unsignedMessageBytes)
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

	return &relayerTypes.WarpLogInfo{
		// BytesToAddress takes the last 20 bytes of the byte array if it is longer than 20 bytes
		SourceAddress:    sourceAddress,
		SourceTxID:       log.TxHash[:],
		UnsignedMsgBytes: unsignedMessageBytes,
		UnsignedMessage:  unsignedMessage,
		BlockNumber:      log.BlockNumber,
	}, nil
}

func (lstnr *Listener) extractAppRelayerMessages(blockInfo relayerTypes.WarpBlockInfo) map[*applicationRelayer][]relayerTypes.WarpLogInfo {
	appRelayerMessages := make(map[*applicationRelayer][]relayerTypes.WarpLogInfo)
	for _, appRelayer := range lstnr.applicationRelayers {
		appRelayerMessages[appRelayer] = make([]relayerTypes.WarpLogInfo, 0)
	}
	for _, warpLog := range blockInfo.WarpLogs {
		// TODONOW: how should we treat errors in this loop?
		warpLogInfo, err := lstnr.NewWarpLogInfo(warpLog)
		if err != nil {
			lstnr.logger.Error(
				"Failed to create warp log info",
				zap.Error(err),
			)
			continue
		}

		// Check that the warp message is from a supported message protocol contract address.
		messageManager, supportedMessageProtocol := lstnr.messageManagers[warpLogInfo.SourceAddress]
		if !supportedMessageProtocol {
			// TODONOW: we should probably return an error here and have the skip logic in the caller
			// Do not return an error here because it is expected for there to be messages from other contracts
			// than just the ones supported by a single listener instance.
			lstnr.logger.Debug(
				"Warp message from unsupported message protocol address. Not relaying.",
				zap.String("protocolAddress", warpLogInfo.SourceAddress.Hex()),
				zap.String("warpMessageID", warpLogInfo.UnsignedMessage.ID().String()),
			)
			continue
		}

		// Fetch the message delivery data
		destinationBlockchainID, err := messageManager.GetDestinationBlockchainID(warpLogInfo.UnsignedMessage)
		if err != nil {
			lstnr.logger.Error(
				"Failed to get destination chain ID",
				zap.Error(err),
			)
			return nil
		}

		originSenderAddress, err := messageManager.GetOriginSenderAddress(warpLogInfo.UnsignedMessage)
		if err != nil {
			lstnr.logger.Error(
				"Failed to get origin sender address",
				zap.Error(err),
			)
			return nil
		}
		destinationAddress, err := messageManager.GetDestinationAddress(warpLogInfo.UnsignedMessage)
		if err != nil {
			lstnr.logger.Error(
				"Failed to get destination address",
				zap.Error(err),
			)
			return nil
		}

		applicationRelayer, ok := lstnr.getApplicationRelayer(
			lstnr.sourceBlockchain.GetBlockchainID(),
			destinationBlockchainID,
			originSenderAddress,
			destinationAddress,
		)
		if !ok {
			lstnr.logger.Debug(
				"Application relayer not found. Skipping message relay.",
				zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.String("destinationBlockchainID", destinationBlockchainID.String()),
				zap.String("originSenderAddress", originSenderAddress.String()),
				zap.String("destinationAddress", destinationAddress.String()),
				zap.String("warpMessageID", warpLogInfo.UnsignedMessage.ID().String()),
			)
			return nil
		}
		appRelayerMessages[applicationRelayer] = append(appRelayerMessages[applicationRelayer], *warpLogInfo)
	}
	return appRelayerMessages
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
			lstnr.logger.Info(
				"Processing block",
				zap.String("originChainId", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Uint64("blockNumber", block.BlockNumber),
			)
			appRelayerMessages := lstnr.extractAppRelayerMessages(block)
			if appRelayerMessages == nil {
				continue
			}
			for appRelayer, warpLogs := range appRelayerMessages {
				// Process the warp logs for each application relayer
				for _, warpLog := range warpLogs {
					err := appRelayer.relayMessage(
						warpLog.UnsignedMessage,
						lstnr.currentRequestID,
						warpLog.MessageManager,
						doneCatchingUp,
						block.BlockNumber,
						true,
					)
					lstnr.currentRequestID++
					if err != nil {
						lstnr.logger.Error(
							"Failed to run application relayer",
							zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
							zap.String("warpMessageID", warpLog.UnsignedMessage.ID().String()),
							zap.Error(err),
						)
					}
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

// Fetch the appropriate application relayer
// Checks for the following registered keys. At most one of these keys should be registered.
// 1. An exact match on sourceBlockchainID, destinationBlockchainID, originSenderAddress, and destinationAddress
// 2. A match on sourceBlockchainID and destinationBlockchainID, with a specific originSenderAddress and any destinationAddress
// 3. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and a specific destinationAddress
// 4. A match on sourceBlockchainID and destinationBlockchainID, with any originSenderAddress and any destinationAddress
func (lstnr *Listener) getApplicationRelayer(
	sourceBlockchainID ids.ID,
	destinationBlockchainID ids.ID,
	originSenderAddress common.Address,
	destinationAddress common.Address,
) (*applicationRelayer, bool) {
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
	applicationRelayer, ok := lstnr.applicationRelayers[applicationRelayerID]
	return applicationRelayer, ok
}

// RouteMessage relays a single warp message to the destination chain. Warp message relay requests from the same origin chain are processed serially
func (lstnr *Listener) RouteMessage(warpLogInfo *relayerTypes.WarpLogInfo, storeProcessedHeight bool) error {
	lstnr.logger.Info(
		"Relaying message",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
	)
	// Unpack the VM message bytes into a Warp message
	unsignedMessage, err := lstnr.contractMessage.UnpackWarpMessage(warpLogInfo.UnsignedMsgBytes)
	if err != nil {
		lstnr.logger.Error(
			"Failed to unpack sender message",
			zap.Error(err),
		)
		return err
	}

	lstnr.logger.Info(
		"Unpacked warp message",
		zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
	)

	// Check that the warp message is from a supported message protocol contract address.
	messageManager, supportedMessageProtocol := lstnr.messageManagers[warpLogInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single listener instance.
		lstnr.logger.Debug(
			"Warp message from unsupported message protocol address. Not relaying.",
			zap.String("protocolAddress", warpLogInfo.SourceAddress.Hex()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		return nil
	}

	// Fetch the message delivery data
	destinationBlockchainID, err := messageManager.GetDestinationBlockchainID(unsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to get destination chain ID",
			zap.Error(err),
		)
		return err
	}

	originSenderAddress, err := messageManager.GetOriginSenderAddress(unsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to get origin sender address",
			zap.Error(err),
		)
		return err
	}
	destinationAddress, err := messageManager.GetDestinationAddress(unsignedMessage)
	if err != nil {
		lstnr.logger.Error(
			"Failed to get destination address",
			zap.Error(err),
		)
		return err
	}

	applicationRelayer, ok := lstnr.getApplicationRelayer(
		lstnr.sourceBlockchain.GetBlockchainID(),
		destinationBlockchainID,
		originSenderAddress,
		destinationAddress,
	)
	if !ok {
		lstnr.logger.Debug(
			"Application relayer not found. Skipping message relay.",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("originSenderAddress", originSenderAddress.String()),
			zap.String("destinationAddress", destinationAddress.String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		return nil
	}

	// Relay the message to the destination. Messages from a given source chain must be processed in serial in order to
	// guarantee that the previous block (n-1) is fully processed by the listener when processing a given log from block n.
	// TODO: Add a config option to use the Warp API, instead of hardcoding to the app request network here
	err = applicationRelayer.relayMessage(
		unsignedMessage,
		lstnr.currentRequestID,
		messageManager,
		storeProcessedHeight,
		warpLogInfo.BlockNumber,
		true,
	)
	if err != nil {
		lstnr.logger.Error(
			"Failed to run application relayer",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}

	// Increment the request ID for the next message relay request
	lstnr.currentRequestID++
	return nil
}
