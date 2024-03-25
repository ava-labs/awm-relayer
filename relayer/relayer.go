// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"encoding/hex"
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
	vms "github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
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

// Relayer handles all messages sent from a given source chain
type Relayer struct {
	Subscriber        vms.Subscriber
	pChainClient      platformvm.Client
	currentRequestID  uint32
	responseChan      chan message.InboundMessage
	contractMessage   vms.ContractMessage
	messageManagers   map[common.Address]messages.MessageManager
	logger            logging.Logger
	sourceBlockchain  config.SourceBlockchain
	catchUpResultChan chan bool
	healthStatus      *atomic.Bool
	globalConfig      *config.Config
	messageRelayers   map[common.Hash]*messageRelayer
}

func NewRelayer(
	logger logging.Logger,
	metrics *MessageRelayerMetrics,
	db database.RelayerDatabase,
	sourceBlockchain config.SourceBlockchain,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	relayerHealth *atomic.Bool,
	cfg *config.Config,
) (*Relayer, error) {
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

	// Marks when the relayer has finished the catch-up process on startup.
	// Until that time, we do not know the order in which messages are processed,
	// since the catch-up process occurs concurrently with normal message processing
	// via the subscriber's Subscribe method. As a result, we cannot safely write the
	// latest processed block to the database without risking missing a block in a fault
	// scenario.
	catchUpResultChan := make(chan bool, 1)

	// Create the message relayers
	messageRelayers := make(map[common.Hash]*messageRelayer)
	for _, relayerKey := range database.GetSourceBlockchainRelayerKeys(&sourceBlockchain) {
		messageRelayer, err := newMessageRelayer(
			logger,
			metrics,
			network,
			messageCreator,
			responseChan,
			relayerKey,
			db,
			sourceBlockchain,
			cfg,
		)
		if err != nil {
			logger.Error(
				"Failed to create message relayer",
				zap.String("relayerKey", relayerKey.CalculateRelayerKey().String()),
				zap.Error(err),
			)
			return nil, err
		}
		messageRelayers[relayerKey.CalculateRelayerKey()] = messageRelayer
	}

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", sourceBlockchain.GetSubnetID().String()),
		zap.String("subnetIDHex", sourceBlockchain.GetSubnetID().Hex()),
		zap.String("blockchainID", sourceBlockchain.GetBlockchainID().String()),
		zap.String("blockchainIDHex", sourceBlockchain.GetBlockchainID().Hex()),
	)
	r := Relayer{
		Subscriber:        sub,
		pChainClient:      pChainClient,
		currentRequestID:  rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		responseChan:      responseChan,
		contractMessage:   vms.NewContractMessage(logger, sourceBlockchain),
		messageManagers:   messageManagers,
		logger:            logger,
		sourceBlockchain:  sourceBlockchain,
		catchUpResultChan: catchUpResultChan,
		healthStatus:      relayerHealth,
		globalConfig:      cfg,
		messageRelayers:   messageRelayers,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err := r.Subscriber.Subscribe(maxSubscribeAttempts)
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, err
	}

	if r.globalConfig.ProcessMissedBlocks {
		height, err := r.calculateListenerStartingBlockHeight(sourceBlockchain.ProcessHistoricalBlocksFromHeight)
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
		go sub.ProcessFromHeight(big.NewInt(0).SetUint64(height), r.catchUpResultChan)
	} else {
		r.logger.Info(
			"processed-missed-blocks set to false, starting processing from chain head",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		)
		err = r.setAllProcessedBlockHeightsToLatest()
		if err != nil {
			logger.Error(
				"Failed to update latest processed block. Continuing to normal relaying operation",
				zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			return nil, err
		}
		r.catchUpResultChan <- true
	}

	return &r, nil
}

func (r *Relayer) calculateListenerStartingBlockHeight(processHistoricalBlocksFromHeight uint64) (uint64, error) {
	minHeight := uint64(0)
	for _, relayer := range r.messageRelayers {
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

func (r *Relayer) setAllProcessedBlockHeightsToLatest() error {
	for _, relayer := range r.messageRelayers {
		_, err := relayer.setProcessedBlockHeightToLatest()
		if err != nil {
			return err
		}
	}
	return nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (r *Relayer) ProcessLogs(ctx context.Context) error {
	doneCatchingUp := false
	for {
		select {
		case catchUpResult := <-r.catchUpResultChan:
			if !catchUpResult {
				r.healthStatus.Store(false)
				r.logger.Error(
					"Failed to catch up on historical blocks. Exiting relayer goroutine.",
					zap.String("originChainId", r.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("failed to catch up on historical blocks")
			} else {
				doneCatchingUp = true
			}
		case txLog := <-r.Subscriber.Logs():
			// Relay the message to the destination chain. Continue on failure.
			r.logger.Info(
				"Handling Teleporter submit message log.",
				zap.String("txId", hex.EncodeToString(txLog.SourceTxID)),
				zap.String("originChainId", r.sourceBlockchain.GetBlockchainID().String()),
				zap.String("sourceAddress", txLog.SourceAddress.String()),
			)

			// Messages are either catch-up messages, or live incoming messages.
			// For live messages, we only write to the database if we're done catching up.
			// This is because during the catch-up process, we cannot guarantee the order
			// of live messages relative to catch-up messages, whereas we know that catch-up
			// messages will always be ordered relative to each other.
			err := r.RelayMessage(&txLog, doneCatchingUp || txLog.IsCatchUpMessage)
			if err != nil {
				r.logger.Error(
					"Error relaying message",
					zap.String("originChainID", r.sourceBlockchain.GetBlockchainID().String()),
					zap.Error(err),
				)
				continue
			}
		case err := <-r.Subscriber.Err():
			r.healthStatus.Store(false)
			r.logger.Error(
				"Received error from subscribed node",
				zap.String("originChainID", r.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			// TODO try to resubscribe in perpetuity once we have a mechanism for refreshing state
			// variables such as Quorum values and processing missed blocks.
			err = r.ReconnectToSubscriber()
			if err != nil {
				r.logger.Error(
					"Relayer goroutine exiting.",
					zap.String("originChainID", r.sourceBlockchain.GetBlockchainID().String()),
					zap.Error(err),
				)
				return fmt.Errorf("relayer goroutine exiting: %w", err)
			}
		case <-ctx.Done():
			r.healthStatus.Store(false)
			r.logger.Info(
				"Exiting Relayer because context cancelled",
				zap.String("originChainId", r.sourceBlockchain.GetBlockchainID().String()),
			)
			return nil
		}
	}
}

// Sets the relayer health status to false while attempting to reconnect.
func (r *Relayer) ReconnectToSubscriber() error {
	// Attempt to reconnect the subscription
	err := r.Subscriber.Subscribe(maxResubscribeAttempts)
	if err != nil {
		return fmt.Errorf("failed to resubscribe to node: %w", err)
	}

	// Success
	r.healthStatus.Store(true)
	return nil
}

// RelayMessage relays a single warp message to the destination chain. Warp message relay requests from the same origin chain are processed serially
func (r *Relayer) RelayMessage(warpLogInfo *vmtypes.WarpLogInfo, storeProcessedHeight bool) error {
	r.logger.Info(
		"Relaying message",
		zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
	)
	// Unpack the VM message bytes into a Warp message
	unsignedMessage, err := r.contractMessage.UnpackWarpMessage(warpLogInfo.UnsignedMsgBytes)
	if err != nil {
		r.logger.Error(
			"Failed to unpack sender message",
			zap.Error(err),
		)
		return err
	}

	r.logger.Info(
		"Unpacked warp message",
		zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
	)

	// Check that the warp message is from a support message protocol contract address.
	messageManager, supportedMessageProtocol := r.messageManagers[warpLogInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single relayer instance.
		r.logger.Debug(
			"Warp message from unsupported message protocol address. Not relaying.",
			zap.String("protocolAddress", warpLogInfo.SourceAddress.Hex()),
		)
		return nil
	}

	// Fetch the message delivery data
	destinationBlockchainID, err := messageManager.GetDestinationBlockchainID(unsignedMessage)
	if err != nil {
		r.logger.Error(
			"Failed to get destination chain ID",
			zap.Error(err),
		)
		return err
	}
	originSenderAddress, err := messageManager.GetOriginSenderAddress(unsignedMessage)
	if err != nil {
		r.logger.Error(
			"Failed to get origin sender address",
			zap.Error(err),
		)
		return err
	}
	destinationAddress, err := messageManager.GetDestinationAddress(unsignedMessage)
	if err != nil {
		r.logger.Error(
			"Failed to get destination address",
			zap.Error(err),
		)
		return err
	}

	// Check that the destination chain ID is supported
	if !r.CheckSupportedDestination(destinationBlockchainID) {
		r.logger.Debug(
			"Message destination chain ID not supported. Not relaying.",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("sourceBlockchain.GetBlockchainID()", destinationBlockchainID.String()),
		)
		return nil
	}

	messageRelayerKey := database.CalculateRelayerKey(
		r.sourceBlockchain.GetBlockchainID(),
		destinationBlockchainID,
		common.Address{}, // TODO: Populate with the proper sender/receiver address
		common.Address{},
	)
	messageRelayer, ok := r.messageRelayers[messageRelayerKey]
	if !ok {
		// TODO: If we don't find the key using the actual addresses, check if all sender/destination addresses are allowed
		r.logger.Error(
			"Message relayer not found",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("originSenderAddress", originSenderAddress.String()),
			zap.String("destinationAddress", destinationAddress.String()),
		)
		return fmt.Errorf("message relayer not found")
	}

	// Relay the message to the destination. Messages from a given source chain must be processed in serial in order to
	// guarantee that the previous block (n-1) is fully processed by the relayer when processing a given log from block n.
	// TODO: Add a config option to use the Warp API, instead of hardcoding to the app request network here
	err = messageRelayer.relayMessage(unsignedMessage, r.currentRequestID, messageManager, storeProcessedHeight, warpLogInfo.BlockNumber, true)
	if err != nil {
		r.logger.Error(
			"Failed to run message relayer",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}

	// Increment the request ID for the next message relay request
	r.currentRequestID++
	return nil
}

// Returns whether destinationBlockchainID is a supported destination.
func (r *Relayer) CheckSupportedDestination(destinationBlockchainID ids.ID) bool {
	supportedDsts := r.sourceBlockchain.GetSupportedDestinations()
	return supportedDsts.Contains(destinationBlockchainID)
}
