// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"
	"math/big"
	"math/rand"
	"sync"

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
	responseChan             <-chan message.InboundMessage
	messageChanLock          *sync.RWMutex
	messageChanMap           map[uint32]chan message.InboundMessage
	errorChan                chan error
	contractMessage          vms.ContractMessage
	messageManagers          map[common.Hash]messages.MessageManager
	logger                   logging.Logger
	db                       database.RelayerDatabase
}

func NewRelayer(
	logger logging.Logger,
	db database.RelayerDatabase,
	sourceSubnetInfo config.SourceSubnet,
	errorChan chan error,
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
		messageChanLock:          new(sync.RWMutex),
		messageChanMap:           make(map[uint32]chan message.InboundMessage),
		errorChan:                errorChan,
		contractMessage:          vms.NewContractMessage(logger, sourceSubnetInfo),
		messageManagers:          messageManagers,
		logger:                   logger,
		db:                       db,
	}

	// Start the message router. We must do this before Subscribing or Initializing for the first time, otherwise we may miss an incoming message
	go r.RouteToMessageChannel()

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

	// Get the latest processed block height from the database.
	var (
		latestSeenBlockData []byte
		latestSeenBlock     *big.Int // initialized to nil
	)
	latestSeenBlockData, err = r.db.Get(r.sourceChainID, []byte(database.LatestSeenBlockKey))

	// The following cases are treated as successful:
	// 1) The database contains the latest seen block data for the chain
	//    - In this case, we parse the block height and process warp logs from that height to the current block
	// 2) The database has been configured for the chain, but does not contain the latest seen block data
	//    - In this case, we save the current block height in the database, but do not process any historical warp logs
	if err == nil {
		// If the database contains the latest seen block data, then  back-process all warp messages from the
		// latest seen block to the latest block
		// This will query the node for any logs that match the filter query from the stored block height,
		r.logger.Info("latest processed block", zap.String("block", string(latestSeenBlockData)))
		var success bool
		latestSeenBlock, success = new(big.Int).SetString(string(latestSeenBlockData), 10)
		if !success {
			r.logger.Error("failed to convert latest block to big.Int", zap.Error(err))
			return nil, nil, err
		}
	} else if errors.Is(err, database.ErrChainNotFound) || errors.Is(err, database.ErrKeyNotFound) {
		// Otherwise, latestSeenBlock is nil, so the call to ProcessFromHeight will simply update the database with the
		// latest block height
		logger.Info(
			"Latest seen block not found in database. Starting from latest block.",
			zap.String("chainID", r.sourceChainID.String()),
		)
	} else {
		r.logger.Warn("failed to get latest block from database", zap.Error(err))
		return nil, nil, err
	}

	// Process historical logs. If this fails for any reason, continue with normal relayer operation, but log the error.
	err = sub.ProcessFromHeight(latestSeenBlock)
	if err != nil {
		logger.Warn(
			"Encountered an error when processing historical blocks. Continuing to normal relaying operation.",
			zap.Error(err),
		)
	}

	return &r, sub, nil
}

// RouteToMessageChannel forwards inbound app messages from the per-subnet responseChan to the per-message messageResponseChan
// The messageResponseChan to forward to is determined by the requestID, which is unique to each message relayer goroutine
// If the requestID has not been added to the map, then the message is dropped.
func (r *Relayer) RouteToMessageChannel() {
	for response := range r.responseChan {
		m := response.Message()
		requestID, ok := message.GetRequestID(m)
		if !ok {
			response.OnFinishedHandling()
			return
		}
		r.messageChanLock.RLock()
		messageResponseChan, ok := r.messageChanMap[requestID]
		r.messageChanLock.RUnlock()
		if ok {
			messageResponseChan <- response
		} else {
			response.OnFinishedHandling()
		}
	}
}

// RelayMessage relays a single warp message to the destination chain. Warp message relay requests are concurrent with each other,
// and synchronized by relayer.
func (r *Relayer) RelayMessage(warpLogInfo *vmtypes.WarpLogInfo, metrics *MessageRelayerMetrics, messageCreator message.Creator) error {
	// Unpack the VM message bytes into a Warp message
	warpMessageInfo, err := r.contractMessage.UnpackWarpMessage(warpLogInfo.UnsignedMsgBytes)
	if err != nil {
		r.logger.Error(
			"Failed to unpack sender message",
			zap.Error(err),
		)
		return err
	}

	// Check that the warp message is from a support message protocol contract address.
	messageManager, supportedMessageProtocol := r.messageManagers[warpLogInfo.SourceAddress]
	if !supportedMessageProtocol {
		// Do not return an error here because it is expected for there to be messages from other contracts
		// than just the ones supported by a single relayer instance.
		r.logger.Debug(
			"Warp message from unsupported message protocol address. not relaying.",
			zap.String("protocolAddress", warpLogInfo.SourceAddress.Hex()),
		)
		return nil
	}

	requestID := r.getAndIncrementRequestID()

	// Add an element to the inbound messages map, keyed by the requestID
	// This allows RouteToMessageChannel to forward inbound messages to the correct channel
	// Note: It's technically possible to block indefinitely here if the inbound handler is processing an flood of non-relevant app responses. This would occur if
	// the RLocks are acquired more quickly than they are released in RouteToMessageChannel. However, given that inbound messages are rate limited and that the
	// RLock is only held for a single map access, this is likely not something we need to worry about in practice.
	messageResponseChan := make(chan message.InboundMessage, peers.InboundMessageChannelSize)
	r.messageChanLock.Lock()
	r.messageChanMap[requestID] = messageResponseChan
	r.messageChanLock.Unlock()

	// Create and run the message relayer to attempt to deliver the message to the destination chain
	messageRelayer := newMessageRelayer(r.logger, metrics, r, warpMessageInfo.WarpUnsignedMessage, warpLogInfo.DestinationChainID, messageResponseChan, messageCreator)
	if err != nil {
		r.logger.Error(
			"Failed to create message relayer",
			zap.Error(err),
		)
		return err
	}

	go messageRelayer.run(warpMessageInfo, requestID, messageManager)

	return nil
}

// Get the current app request ID and increment it. Request IDs need to be unique to each teleporter message, but the specific values do not matter.
// This should only be called by the subnet-level TeleporterRelayer, and not by the goroutines that handle individual teleporter messages
func (r *Relayer) getAndIncrementRequestID() uint32 {
	id := r.currentRequestID
	r.currentRequestID++
	return id
}
