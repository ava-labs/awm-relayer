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
	messageManagers          map[common.Hash]messages.MessageManager
	logger                   logging.Logger
	metrics                  *MessageRelayerMetrics
	db                       database.RelayerDatabase
	supportedDestinations    set.Set[ids.ID]
	rpcEndpoint              string
	apiNodeURI               string
	messageCreator           message.Creator
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
	messageManagers := make(map[common.Hash]messages.MessageManager)
	for address, config := range sourceSubnetInfo.MessageContracts {
		addressHash := common.HexToHash(address)
		messageManager, err := messages.NewMessageManager(logger, addressHash, config, filteredDestinationClients)
		if err != nil {
			logger.Error(
				"Failed to create message manager",
				zap.Error(err),
			)
			return nil, err
		}
		messageManagers[addressHash] = messageManager
	}

	rpcEndpoint := sourceSubnetInfo.GetNodeRPCEndpoint()
	uri := utils.StripFromString(rpcEndpoint, "/ext")

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
		err = sub.ProcessFromHeight(big.NewInt(0).SetUint64(height))
		if err != nil {
			logger.Error(
				"Failed to process blocks from height on startup",
				zap.Error(err),
			)
			return nil, err
	} else {
		err = r.setProcessedBlockHeightToLatest()
		if err != nil {
			logger.Warn(
				"Failed to update latest processed block. Continuing to normal relaying operation",
				zap.String("blockchainID", r.sourceBlockchainID.String()),
				zap.Error(err),
			)
			return nil, err
		}
	}

	return &r, nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (r *Relayer) ProcessLogs(ctx context.Context) error {
	for {
		select {
		case txLog := <-r.Subscriber.Logs():
			r.logger.Info(
				"Handling Teleporter submit message log.",
				zap.String("txId", hex.EncodeToString(txLog.SourceTxID)),
				zap.String("originChainId", r.sourceBlockchainID.String()),
				zap.String("sourceAddress", txLog.SourceAddress.String()),
			)

			// Relay the message to the destination chain. Continue on failure.
			err := r.RelayMessage(&txLog)
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
	// Attempt to get the latest processed block height from the database.
	// Note that there may be unrelayed messages in the latest processed block
	// because it is updated as soon as a single message from that block is relayed,
	// and there may be multiple message in the same block.
	latestProcessedBlockData, err := r.db.Get(r.sourceBlockchainID, []byte(database.LatestProcessedBlockKey))
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
		r.logger.Warn(
			"failed to get latest block from database",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.Error(err),
		)
		return 0, err
	}

	// If the database does contain the latest processed block data for the chain,
	// use the max of the latest processed block and the configured start block height (if it was provided)
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		r.logger.Error("failed to parse Uint from the database", zap.Error(err))
		return 0, err
	}
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

	err = r.db.Put(r.sourceBlockchainID, []byte(database.LatestProcessedBlockKey), []byte(strconv.FormatUint(latestBlock, 10)))
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
func (r *Relayer) RelayMessage(warpLogInfo *vmtypes.WarpLogInfo) error {
	r.logger.Info(
		"Relaying message",
		zap.String("blockchainID", r.sourceBlockchainID.String()),
	)
	// Unpack the VM message bytes into a Warp message
	warpMessageInfo, err := r.contractMessage.UnpackWarpMessage(warpLogInfo.UnsignedMsgBytes)
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
		zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
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

	destinationBlockchainID, err := messageManager.GetDestinationBlockchainID(warpMessageInfo)
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
	messageRelayer := newMessageRelayer(r, warpMessageInfo.WarpUnsignedMessage, destinationBlockchainID)
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
	err = messageRelayer.relayMessage(warpMessageInfo, r.currentRequestID, messageManager, true)
	if err != nil {
		r.logger.Error(
			"Failed to run message relayer",
			zap.String("blockchainID", r.sourceBlockchainID.String()),
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}

	// Increment the request ID for the next message relay request
	r.currentRequestID++

	// Update the database with the latest processed block height
	err = r.db.Put(r.sourceBlockchainID, []byte(database.LatestProcessedBlockKey), []byte(strconv.FormatUint(warpLogInfo.BlockNumber, 10)))
	if err != nil {
		r.logger.Error(
			fmt.Sprintf("failed to put %s into database", database.LatestProcessedBlockKey),
			zap.Error(err),
		)
	}

	return nil
}

// Returns whether destinationBlockchainID is a supported destination.
// If supportedDestinations is empty, then all destination chain IDs are supported.
func (r *Relayer) CheckSupportedDestination(destinationBlockchainID ids.ID) bool {
	return len(r.supportedDestinations) == 0 || r.supportedDestinations.Contains(destinationBlockchainID)
}
