// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strconv"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/utils"
	vms "github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

var (
	// Errors
	ErrNoStartBlock = errors.New("database does not contain latest processed block data and startBlockHeight is unset.")
)

const (
	maxSubscribeAttempts = 10
	// TODO attempt to resubscribe in perpetuity once we are able to process missed blocks and
	// refresh the chain config on reconnect.
	maxResubscribeAttempts = 10
)

// Relayer handles all messages sent from a given source chain
type Relayer struct {
	Subscriber               vms.Subscriber
	pChainClient             platformvm.Client
	canonicalValidatorClient *CanonicalValidatorClient
	currentRequestID         uint32
	network                  *peers.AppRequestNetwork
	sourceSubnetID           ids.ID
	sourceBlockchainID       ids.ID
	responseChan             chan message.InboundMessage
	contractMessage          vms.ContractMessage
	messageManagers          map[common.Address]messages.MessageManager
	logger                   logging.Logger
	metrics                  *MessageRelayerMetrics
	db                       database.RelayerDatabase
	supportedDestinations    set.Set[ids.ID]
	rpcEndpoint              string
	apiNodeURI               string
	messageCreator           message.Creator
	catchUpResultChan        chan bool
	healthStatus             *atomic.Bool
}

func NewRelayer(
	logger logging.Logger,
	metrics *MessageRelayerMetrics,
	db database.RelayerDatabase,
	sourceSubnetInfo config.SourceSubnet,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	shouldProcessMissedBlocks bool,
	relayerHealth *atomic.Bool,
) (*Relayer, error) {
	sub := vms.NewSubscriber(logger, sourceSubnetInfo)

	subnetID, err := ids.FromString(sourceSubnetInfo.SubnetID)
	if err != nil {
		logger.Error(
			"Invalid subnetID in configuration",
			zap.Error(err),
		)
		return nil, err
	}

	blockchainID, err := ids.FromString(sourceSubnetInfo.BlockchainID)
	if err != nil {
		logger.Error(
			"Failed to decode base-58 encoded source chain ID",
			zap.Error(err),
		)
		return nil, err
	}

	var filteredDestinationClients map[ids.ID]vms.DestinationClient
	supportedDestinationsBlockchainIDs := sourceSubnetInfo.GetSupportedDestinations()
	if len(supportedDestinationsBlockchainIDs) > 0 {
		filteredDestinationClients := make(map[ids.ID]vms.DestinationClient)
		for id := range supportedDestinationsBlockchainIDs {
			filteredDestinationClients[id] = destinationClients[id]
		}
	} else {
		filteredDestinationClients = destinationClients
	}

	// Create message managers for each supported message protocol
	messageManagers := make(map[common.Address]messages.MessageManager)
	for addressStr, config := range sourceSubnetInfo.MessageContracts {
		address := common.HexToAddress(addressStr)
		messageManager, err := messages.NewMessageManager(logger, address, config, filteredDestinationClients)
		if err != nil {
			logger.Error(
				"Failed to create message manager",
				zap.Error(err),
			)
			return nil, err
		}
		messageManagers[address] = messageManager
	}

	rpcEndpoint := sourceSubnetInfo.GetNodeRPCEndpoint()
	uri := utils.StripFromString(rpcEndpoint, "/ext")

	// Marks when the relayer has finished the catch-up process on startup.
	// Until that time, we do not know the order in which messages are processed,
	// since the catch-up process occurs concurrently with normal message processing
	// via the subscriber's Subscribe method. As a result, we cannot safely write the
	// latest processed block to the database without risking missing a block in a fault
	// scenario.
	catchUpResultChan := make(chan bool, 1)

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", subnetID.String()),
		zap.String("subnetIDHex", subnetID.Hex()),
		zap.String("blockchainID", blockchainID.String()),
		zap.String("blockchainIDHex", blockchainID.Hex()),
	)
	r := Relayer{
		Subscriber:               sub,
		pChainClient:             pChainClient,
		canonicalValidatorClient: NewCanonicalValidatorClient(logger, pChainClient),
		currentRequestID:         rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		network:                  network,
		sourceSubnetID:           subnetID,
		sourceBlockchainID:       blockchainID,
		responseChan:             responseChan,
		contractMessage:          vms.NewContractMessage(logger, sourceSubnetInfo),
		messageManagers:          messageManagers,
		logger:                   logger,
		metrics:                  metrics,
		db:                       db,
		supportedDestinations:    supportedDestinationsBlockchainIDs,
		rpcEndpoint:              rpcEndpoint,
		apiNodeURI:               uri,
		messageCreator:           messageCreator,
		catchUpResultChan:        catchUpResultChan,
		healthStatus:             relayerHealth,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err = r.Subscriber.Subscribe(maxSubscribeAttempts)
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, err
	}

	if shouldProcessMissedBlocks {
		height, err := r.calculateStartingBlockHeight(sourceSubnetInfo.StartBlockHeight)
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
		err = r.setProcessedBlockHeightToLatest()
		if err != nil {
			logger.Error(
				"Failed to update latest processed block. Continuing to normal relaying operation",
				zap.String("blockchainID", r.sourceBlockchainID.String()),
				zap.Error(err),
			)
			return nil, err
		}
		r.catchUpResultChan <- true
	}

	return &r, nil
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
					zap.String("originChainId", r.sourceBlockchainID.String()),
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
				zap.String("originChainId", r.sourceBlockchainID.String()),
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
					zap.String("originChainID", r.sourceBlockchainID.String()),
					zap.Error(err),
				)
				continue
			}
		case err := <-r.Subscriber.Err():
			r.healthStatus.Store(false)
			r.logger.Error(
				"Received error from subscribed node",
				zap.String("originChainID", r.sourceBlockchainID.String()),
				zap.Error(err),
			)
			// TODO try to resubscribe in perpetuity once we have a mechanism for refreshing state
			// variables such as Quorum values and processing missed blocks.
			err = r.ReconnectToSubscriber()
			if err != nil {
				r.logger.Error(
					"Relayer goroutine exiting.",
					zap.String("originChainID", r.sourceBlockchainID.String()),
					zap.Error(err),
				)
				return fmt.Errorf("relayer goroutine exiting: %w", err)
			}
		case <-ctx.Done():
			r.healthStatus.Store(false)
			r.logger.Info(
				"Exiting Relayer because context cancelled",
				zap.String("originChainId", r.sourceBlockchainID.String()),
			)
			return nil
		}
	}
}

// Determines the height to process from. There are two cases:
// 1) The database contains the latest processed block data for the chain
//   - In this case, we return the maximum of the latest processed block and the configured start block height
//
// 2) The database has been configured for the chain, but does not contain the latest processed block data
//   - In this case, we return the configured start block height
func (r *Relayer) calculateStartingBlockHeight(startBlockHeight uint64) (uint64, error) {
	latestProcessedBlock, err := r.getLatestProcessedBlockHeight()
	if errors.Is(err, database.ErrChainNotFound) || errors.Is(err, database.ErrKeyNotFound) {
		// The database does not contain the latest processed block data for the chain, so use the configured StartBlockHeight instead
		if startBlockHeight == 0 {
			r.logger.Warn(
				"database does not contain latest processed block data and startBlockHeight is unset. Please provide a non-zero startBlockHeight in the configuration.",
				zap.String("blockchainID", r.sourceBlockchainID.String()),
			)
			return 0, ErrNoStartBlock
		}
		return startBlockHeight, nil
	} else if err != nil {
		// Otherwise, we've encountered an unknown database error
		r.logger.Error(
			"failed to get latest block from database",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return 0, err
	}

	// If the database does contain the latest processed block data for the chain,
	// use the max of the latest processed block and the configured start block height (if it was provided)
	if latestProcessedBlock > startBlockHeight {
		r.logger.Info(
			"Processing historical blocks from the latest processed block in the DB",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Uint64("latestProcessedBlock", latestProcessedBlock),
		)
		return latestProcessedBlock, nil
	}
	// Otherwise, return the configured start block height
	r.logger.Info(
		"Processing historical blocks from the configured start block height",
		zap.String("blockchainID", r.sourceBlockchainID.String()),
		zap.Uint64("startBlockHeight", startBlockHeight),
	)
	return startBlockHeight, nil
}

func (r *Relayer) setProcessedBlockHeightToLatest() error {
	ethClient, err := ethclient.Dial(r.rpcEndpoint)
	if err != nil {
		r.logger.Error(
			"Failed to dial node",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return err
	}

	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		r.logger.Error(
			"Failed to get latest block",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return err
	}

	r.logger.Info(
		"Updating latest processed block in database",
		zap.String("blockchainID", r.sourceBlockchainID.String()),
		zap.Uint64("latestBlock", latestBlock),
	)

	err = r.storeBlockHeight(latestBlock)
	if err != nil {
		r.logger.Error(
			fmt.Sprintf("failed to put %s into database", database.LatestProcessedBlockKey),
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return err
	}
	return nil
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
		zap.String("blockchainID", r.sourceBlockchainID.String()),
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
		zap.String("blockchainID", r.sourceBlockchainID.String()),
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

	destinationBlockchainID, err := messageManager.GetDestinationBlockchainID(unsignedMessage)
	if err != nil {
		r.logger.Error(
			"Failed to get destination chain ID",
			zap.Error(err),
		)
		return err
	}

	// Check that the destination chain ID is supported
	if !r.CheckSupportedDestination(destinationBlockchainID) {
		r.logger.Debug(
			"Message destination chain ID not supported. Not relaying.",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		)
		return nil
	}

	// Create and run the message relayer to attempt to deliver the message to the destination chain
	messageRelayer := newMessageRelayer(r, unsignedMessage, destinationBlockchainID)
	if err != nil {
		r.logger.Error(
			"Failed to create message relayer",
			zap.Error(err),
		)
		return err
	}

	// Relay the message to the destination. Messages from a given source chain must be processed in serial in order to
	// guarantee that the previous block (n-1) is fully processed by the relayer when processing a given log from block n.
	// TODO: Add a config option to use the Warp API, instead of hardcoding to the app request network here
	err = messageRelayer.relayMessage(unsignedMessage, r.currentRequestID, messageManager, true)
	if err != nil {
		r.logger.Error(
			"Failed to run message relayer",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}

	// Increment the request ID for the next message relay request
	r.currentRequestID++

	if !storeProcessedHeight {
		return nil
	}

	// Update the database with the latest processed block height
	return r.storeLatestBlockHeight(warpLogInfo.BlockNumber)
}

// Returns whether destinationBlockchainID is a supported destination.
// If supportedDestinations is empty, then all destination chain IDs are supported.
func (r *Relayer) CheckSupportedDestination(destinationBlockchainID ids.ID) bool {
	return len(r.supportedDestinations) == 0 || r.supportedDestinations.Contains(destinationBlockchainID)
}

// Get the latest processed block height from the database.
// Note that there may be unrelayed messages in the latest processed block
// because it is updated as soon as a single message from that block is relayed,
// and there may be multiple message in the same block.
func (r *Relayer) getLatestProcessedBlockHeight() (uint64, error) {
	latestProcessedBlockData, err := r.db.Get(r.sourceBlockchainID, []byte(database.LatestProcessedBlockKey))
	if err != nil {
		return 0, err
	}
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		return 0, err
	}
	return latestProcessedBlock, nil
}

// Store the block height in the database. Does not check against the current latest processed block height.
func (r *Relayer) storeBlockHeight(height uint64) error {
	return r.db.Put(r.sourceBlockchainID, []byte(database.LatestProcessedBlockKey), []byte(strconv.FormatUint(height, 10)))
}

// Stores the block height in the database if it is greater than the current latest processed block height.
func (r *Relayer) storeLatestBlockHeight(height uint64) error {
	// First, check that the stored height is less than the current block height
	// This is necessary because the relayer may be processing blocks out of order on startup
	latestProcessedBlock, err := r.getLatestProcessedBlockHeight()
	if err != nil && !errors.Is(err, database.ErrChainNotFound) && !errors.Is(err, database.ErrKeyNotFound) {
		r.logger.Error(
			"Encountered an unknown error while getting latest processed block from database",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return err
	}

	// An unhandled error at this point indicates the DB does not store the block data for the chain,
	// so we write unconditionally in that case. Otherwise, we only overwrite if the new height is greater
	// than the stored height.
	if err != nil || height > latestProcessedBlock {
		err = r.storeBlockHeight(height)
		if err != nil {
			r.logger.Error(
				fmt.Sprintf("failed to put %s into database", database.LatestProcessedBlockKey),
				zap.Error(err),
			)
		}
		return err
	}
	return nil
}
