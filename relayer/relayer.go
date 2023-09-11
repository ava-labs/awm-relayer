// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"math/rand"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
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
}

func NewRelayer(
	logger logging.Logger,
	sourceSubnetInfo config.SourceSubnet,
	errorChan chan error,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*Relayer, vms.Subscriber, error) {

	sub := vms.NewSubscriber(logger, sourceSubnetInfo)

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
		messageManager, err := messages.NewMessageManager(logger,
			addressHash,
			config,
			destinationClients,
			sourceSubnetInfo,
		)
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
	}

	// Start the message router. We must do this before Subscribing for the first time, otherwise we may miss an incoming message
	go r.RouteToMessageChannel()

	err = sub.Subscribe()
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, nil, err
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
