// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strconv"

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
	"go.uber.org/zap"
)

// Relayer handles all messages sent from a given source chain
type Relayer struct {
	pChainClient             platformvm.Client
	canonicalValidatorClient *CanonicalValidatorClient
	currentRequestID         uint32
	network                  *peers.AppRequestNetwork
	sourceSubnetID           ids.ID
	sourceChainID            ids.ID
	responseChan             chan message.InboundMessage
	contractMessage          vms.ContractMessage
	messageManagers          map[common.Hash]messages.MessageManager
	logger                   logging.Logger
	db                       database.RelayerDatabase
}

func NewRelayer(
	logger logging.Logger,
	db database.RelayerDatabase,
	sourceSubnetInfo config.SourceSubnet,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*Relayer, vms.Subscriber, error) {

	sub := vms.NewSubscriber(logger, sourceSubnetInfo, db)

	subnetID, err := ids.FromString(sourceSubnetInfo.SubnetID)
	if err != nil {
		logger.Error(
			"Invalid subnetID in configuration",
			zap.Error(err),
		)
		return nil, nil, err
	}

	chainID, err := ids.FromString(sourceSubnetInfo.ChainID)
	if err != nil {
		logger.Error(
			"Failed to decode base-58 encoded source chain ID",
			zap.Error(err),
		)
		return nil, nil, err
	}

	// Create message managers for each supported message protocol
	messageManagers := make(map[common.Hash]messages.MessageManager)
	for address, config := range sourceSubnetInfo.MessageContracts {
		addressHash := common.HexToHash(address)
		messageManager, err := messages.NewMessageManager(logger, addressHash, config, destinationClients)
		if err != nil {
			logger.Error(
				"Failed to create message manager",
				zap.Error(err),
			)
			return nil, nil, err
		}
		messageManagers[addressHash] = messageManager
	}

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", subnetID.String()),
		zap.String("subnetIDHex", subnetID.Hex()),
		zap.String("chainID", chainID.String()),
		zap.String("chainIDHex", chainID.Hex()),
	)
	r := Relayer{
		pChainClient:             pChainClient,
		canonicalValidatorClient: NewCanonicalValidatorClient(pChainClient),
		currentRequestID:         rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		network:                  network,
		sourceSubnetID:           subnetID,
		sourceChainID:            chainID,
		responseChan:             responseChan,
		contractMessage:          vms.NewContractMessage(logger, sourceSubnetInfo),
		messageManagers:          messageManagers,
		logger:                   logger,
		db:                       db,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err = sub.Subscribe()
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, nil, err
	}

	// Get the latest seen block height from the database.
	latestSeenBlockData, err := r.db.Get(r.sourceChainID, []byte(database.LatestSeenBlockKey))

	// The following cases are treated as successful:
	// 1) The database contains the latest seen block data for the chain
	//    - In this case, we parse the block height and process warp logs from that height to the current block
	// 2) The database has been configured for the chain, but does not contain the latest seen block data
	//    - In this case, we save the current block height in the database, but do not process any historical warp logs
	if err == nil {
		// If the database contains the latest seen block data, then back-process all warp messages from that block to the latest block
		// This will query the node for any logs that match the filter query in that range
		latestSeenBlock, success := new(big.Int).SetString(string(latestSeenBlockData), 10)
		if !success {
			r.logger.Error("failed to convert latest block to big.Int", zap.Error(err))
			return nil, nil, err
		}

		err = sub.ProcessFromHeight(latestSeenBlock)
		if err != nil {
			logger.Warn(
				"Encountered an error when processing historical blocks. Continuing to normal relaying operation.",
				zap.String("chainID", r.sourceChainID.String()),
				zap.Error(err),
			)
		}
		return &r, sub, nil
	}
	if errors.Is(err, database.ErrChainNotFound) || errors.Is(err, database.ErrKeyNotFound) {
		// Otherwise, latestSeenBlock is nil, so the call to ProcessFromHeight will simply update the database with the
		// latest block height
		logger.Info(
			"Latest seen block not found in database. Starting from latest block.",
			zap.String("chainID", r.sourceChainID.String()),
		)

		err := sub.UpdateLatestSeenBlock()
		if err != nil {
			logger.Warn(
				"Failed to update latest seen block. Continuing to normal relaying operation",
				zap.String("chainID", r.sourceChainID.String()),
				zap.Error(err),
			)
		}
		return &r, sub, nil
	}

	// If neither of the above conditions are met, then we return an error
	r.logger.Warn(
		"failed to get latest block from database",
		zap.String("chainID", r.sourceChainID.String()),
		zap.Error(err),
	)
	return nil, nil, err
}

// RelayMessage relays a single warp message to the destination chain. Warp message relay requests are concurrent with each other,
// and synchronized by relayer.
func (r *Relayer) RelayMessage(warpLogInfo *vmtypes.WarpLogInfo, metrics *MessageRelayerMetrics, messageCreator message.Creator) error {
	r.logger.Info(
		"Relaying message",
		zap.String("chainID", r.sourceChainID.String()),
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
		zap.String("chainID", r.sourceChainID.String()),
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

	// Create and run the message relayer to attempt to deliver the message to the destination chain
	messageRelayer := newMessageRelayer(r.logger, metrics, r, warpMessageInfo.WarpUnsignedMessage, warpLogInfo.DestinationChainID, r.responseChan, messageCreator)
	if err != nil {
		r.logger.Error(
			"Failed to create message relayer",
			zap.Error(err),
		)
		return err
	}

	// Relay the message to the destination. Messages from a given source chain must be processed in serial in order to
	// guarantee that the previous block (n-1) is fully processed by the relayer when processing a given log from block n.
	err = messageRelayer.relayMessage(warpMessageInfo, r.currentRequestID, messageManager)
	if err != nil {
		r.logger.Error(
			"Failed to run message relayer",
			zap.String("chainID", r.sourceChainID.String()),
			zap.String("warpMessageID", warpMessageInfo.WarpUnsignedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}

	// Increment the request ID for the next message relay request
	r.currentRequestID++

	// Update the database with the latest seen block height
	// We cannot store the latest processed block height, because we do not know if a given log is the last log in a block
	err = r.db.Put(r.sourceChainID, []byte(database.LatestSeenBlockKey), []byte(strconv.FormatUint(warpLogInfo.BlockNumber, 10)))
	if err != nil {
		r.logger.Error(
			fmt.Sprintf("failed to put %s into database", database.LatestSeenBlockKey),
			zap.Error(err),
		)
	}

	return nil
}
