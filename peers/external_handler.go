// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	avalancheCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var _ router.ExternalHandler = &RelayerExternalHandler{}

// Note: all of the external handler's methods are called on peer goroutines. It
// is possible for multiple concurrent calls to happen with different NodeIDs.
// However, a given NodeID will only be performing one call at a time.
type RelayerExternalHandler struct {
	log            logging.Logger
	lock           *sync.Mutex
	responseChans  map[uint32]chan message.InboundMessage
	responsesCount map[uint32]expectedResponses
	timeoutManager timer.AdaptiveTimeoutManager
}

// expectedResponses counts the number of responses and compares against the expected number of responses
type expectedResponses struct {
	expected, received int
}

// Create a new RelayerExternalHandler to forward relevant inbound app messages to the respective
// Teleporter application relayer, as well as handle timeouts.
func NewRelayerExternalHandler(
	logger logging.Logger,
	registerer prometheus.Registerer,
) (*RelayerExternalHandler, error) {
	// TODO: Leaving this static for now, but we may want to have this as a config option
	cfg := timer.AdaptiveTimeoutConfig{
		InitialTimeout:     constants.DefaultNetworkInitialTimeout,
		MinimumTimeout:     constants.DefaultNetworkInitialTimeout,
		MaximumTimeout:     constants.DefaultNetworkMaximumTimeout,
		TimeoutCoefficient: constants.DefaultNetworkTimeoutCoefficient,
		TimeoutHalflife:    constants.DefaultNetworkTimeoutHalflife,
	}

	timeoutManager, err := timer.NewAdaptiveTimeoutManager(&cfg, "external_handler", registerer)
	if err != nil {
		logger.Error(
			"Failed to create timeout manager",
			zap.Error(err),
		)
		return nil, err
	}

	go timeoutManager.Dispatch()

	return &RelayerExternalHandler{
		log:            logger,
		lock:           &sync.Mutex{},
		responseChans:  make(map[uint32]chan message.InboundMessage),
		responsesCount: make(map[uint32]expectedResponses),
		timeoutManager: timeoutManager,
	}, nil
}

// HandleInbound handles all inbound app message traffic. For the relayer, we only care about App Responses to
// signature request App Requests, and App Request Fail messages sent by the timeout manager.
// For each inboundMessage, OnFinishedHandling must be called exactly once. However, since we handle relayer messages
// async, we must call OnFinishedHandling manually across all code paths.
//
// This diagram illustrates how HandleInbound forwards relevant AppResponses to the corresponding
// Teleporter application relayer. On startup, one Relayer goroutine is created per source subnet,
// which listens to the subscriber for cross-chain messages. When a cross-chain message is picked
// up by a Relayer, HandleInbound routes AppResponses traffic to the appropriate Relayer.
func (h *RelayerExternalHandler) HandleInbound(_ context.Context, inboundMessage message.InboundMessage) {
	h.log.Debug(
		"Handling app response",
		zap.Stringer("op", inboundMessage.Op()),
		zap.Stringer("from", inboundMessage.NodeID()),
	)
	if inboundMessage.Op() == message.AppResponseOp || inboundMessage.Op() == message.AppErrorOp {
		h.registerAppResponse(inboundMessage)
	} else {
		h.log.Debug("Ignoring message", zap.Stringer("op", inboundMessage.Op()))
		inboundMessage.OnFinishedHandling()
	}
}

func (h *RelayerExternalHandler) Connected(nodeID ids.NodeID, version *version.Application, subnetID ids.ID) {
	h.log.Info(
		"Connected",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("version", version),
		zap.Stringer("subnetID", subnetID),
	)
}

func (h *RelayerExternalHandler) Disconnected(nodeID ids.NodeID) {
	h.log.Info(
		"Disconnected",
		zap.Stringer("nodeID", nodeID),
	)
}

// RegisterRequestID registers an AppRequest by requestID, and marks the number of
// expected responses, equivalent to the number of nodes requested. requestID should
// be globally unique for the lifetime of the AppRequest. This is upper bounded by the timeout duration.
func (h *RelayerExternalHandler) RegisterRequestID(
	requestID uint32,
	numExpectedResponses int,
) chan message.InboundMessage {
	// Create a channel to receive the response
	h.lock.Lock()
	defer h.lock.Unlock()

	h.log.Debug("Registering request ID", zap.Uint32("requestID", requestID))

	responseChan := make(chan message.InboundMessage, numExpectedResponses)
	h.responseChans[requestID] = responseChan
	h.responsesCount[requestID] = expectedResponses{
		expected: numExpectedResponses,
	}
	return responseChan
}

// RegisterRequest registers an AppRequest with the timeout manager.
// If RegisterResponse is not called before the timeout, HandleInbound is called with
// an internally created AppRequestFailed message.
func (h *RelayerExternalHandler) RegisterAppRequest(reqID ids.RequestID) {
	inMsg := message.InboundAppError(
		reqID.NodeID,
		reqID.SourceChainID,
		reqID.RequestID,
		avalancheCommon.ErrTimeout.Code,
		avalancheCommon.ErrTimeout.Message,
	)
	h.timeoutManager.Put(reqID, false, func() {
		h.HandleInbound(context.Background(), inMsg)
	})
}

// RegisterResponse registers an AppResponse with the timeout manager
func (h *RelayerExternalHandler) registerAppResponse(inboundMessage message.InboundMessage) {
	h.lock.Lock()
	defer h.lock.Unlock()

	// Extract the message fields
	m := inboundMessage.Message()

	// Get the blockchainID from the message.
	// Note: we should NOT call GetSourceBlockchainID; this is for cross-chain messages using the vm2 interface
	// For normal app requests messages, the calls result in the same value, but if the relayer handles an
	// inbound cross-chain app message, then we would get the incorrect chain ID.
	blockchainID, err := message.GetChainID(m)
	if err != nil {
		h.log.Error("Could not get blockchainID from message")
		inboundMessage.OnFinishedHandling()
		return
	}
	sourceBlockchainID, err := message.GetSourceChainID(m)
	if err != nil {
		h.log.Error("Could not get sourceBlockchainID from message")
		inboundMessage.OnFinishedHandling()
		return
	}
	requestID, ok := message.GetRequestID(m)
	if !ok {
		h.log.Error("Could not get requestID from message")
		inboundMessage.OnFinishedHandling()
		return
	}

	// Remove the timeout on the request
	reqID := ids.RequestID{
		NodeID:             inboundMessage.NodeID(),
		SourceChainID:      sourceBlockchainID,
		DestinationChainID: blockchainID,
		RequestID:          requestID,
		Op:                 byte(inboundMessage.Op()),
	}
	h.timeoutManager.Remove(reqID)

	// Dispatch to the appropriate response channel
	if responseChan, ok := h.responseChans[requestID]; ok {
		responseChan <- inboundMessage
	} else {
		h.log.Debug("Could not find response channel for request", zap.Uint32("requestID", requestID))
		return
	}

	// Check for the expected number of responses, and clear from the map if all expected responses have been received
	// TODO: we can improve performance here by independently locking the response channel and response count maps
	responses, ok := h.responsesCount[requestID]
	if !ok {
		h.log.Error("Could not find expected responses for request", zap.Uint32("requestID", requestID))
		return
	}
	received := responses.received + 1
	if received == responses.expected {
		close(h.responseChans[requestID])
		delete(h.responseChans, requestID)
		delete(h.responsesCount, requestID)
	} else {
		h.responsesCount[requestID] = expectedResponses{
			expected: responses.expected,
			received: received,
		}
	}
}
