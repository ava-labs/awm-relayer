// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var _ router.ExternalHandler = (*RelayerExternalHandler)(nil)

// Note: all of the external handler's methods are called on peer goroutines. It
// is possible for multiple concurrent calls to happen with different NodeIDs.
// However, a given NodeID will only be performing one call at a time.
type RelayerExternalHandler struct {
	log               logging.Logger
	responseChansLock *sync.RWMutex
	responseChans     map[ids.ID]chan message.InboundMessage
	timeoutManager    timer.AdaptiveTimeoutManager
}

// Create a new RelayerExternalHandler to forward relevant inbound app messages to the respective Teleporter message relayer, as well as handle timeouts.
func NewRelayerExternalHandler(
	logger logging.Logger,
	registerer prometheus.Registerer,
	responseChans map[ids.ID]chan message.InboundMessage,
	responseChansLock *sync.RWMutex,
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
		log:               logger,
		responseChansLock: responseChansLock,
		responseChans:     responseChans,
		timeoutManager:    timeoutManager,
	}, nil
}

// HandleInbound handles all inbound app message traffic. For the relayer, we only care about App Responses to
// signature request App Requests, and App Request Fail messages sent by the timeout manager.
// For each inboundMessage, OnFinishedHandling must be called exactly once. However, since we handle relayer messages
// async, we must call OnFinishedHandling manually across all code paths.
//
// This diagram illustrates how HandleInbound forwards relevant AppResponses to the corresponding Teleporter message relayer.
// On startup, one TeleporterRelayer goroutine is created per source subnet, which listens to the subscriber for cross-chain messages
// When a cross-chain message is picked up by a TeleporterRelayer, a teleporterMessageRelayer goroutine is created and closed
// once the cross-chain message is delivered. Messages are routed through the resulting tree structure:
//
// 								  TeleporterRelayer/source subnet       teleporterMessageRelayer/Teleporter message
//
//                       		{ TeleporterRelayer.responseChan --...  { teleporterMessageRelayer.messageResponseChan (consumer)
// HandleInbound (producer) --->{ TeleporterRelayer.responseChan ------>{ teleporterMessageRelayer.messageResponseChan (consumer)
//                       		{			  ...					    {						...
//                       		{ TeleporterRelayer.responseChan --...  { teleporterMessageRelayer.messageResponseChan (consumer)

func (h *RelayerExternalHandler) HandleInbound(_ context.Context, inboundMessage message.InboundMessage) {
	h.log.Debug(
		"receiving message",
		zap.Stringer("op", inboundMessage.Op()),
	)
	if inboundMessage.Op() == message.AppResponseOp || inboundMessage.Op() == message.AppRequestFailedOp {
		h.log.Info("handling app response", zap.Stringer("from", inboundMessage.NodeID()))

		// Extract the message fields
		m := inboundMessage.Message()

		// Get the ChainID from the message.
		// Note: we should NOT call GetSourceChainID; this is for cross-chain messages using the vm2 interface
		// For normal app requests messages, the calls result in the same value, but if the relayer handles an
		// inbound cross-chain app message, then we would get the incorrect chain ID.
		chainID, err := message.GetChainID(m)
		if err != nil {
			h.log.Error("could not get chainID from message")
			inboundMessage.OnFinishedHandling()
			return
		}
		sourceChainID, err := message.GetSourceChainID(m)
		if err != nil {
			h.log.Error("could not get sourceChainID from message")
			inboundMessage.OnFinishedHandling()
			return
		}
		requestID, ok := message.GetRequestID(m)
		if !ok {
			h.log.Error("could not get requestID from message")
			inboundMessage.OnFinishedHandling()
			return
		}

		reqID := ids.RequestID{
			NodeID:             inboundMessage.NodeID(),
			SourceChainID:      sourceChainID,
			DestinationChainID: chainID,
			RequestID:          requestID,
			Op:                 byte(inboundMessage.Op()),
		}
		h.RegisterResponse(reqID)

		// Route to the appropriate response channel. Do not block on this call, otherwise incoming message handling may be blocked
		// OnFinishedHandling is called by the consumer of the response channel
		go func(message.InboundMessage, ids.ID) {
			h.responseChansLock.RLock()
			defer h.responseChansLock.RUnlock()

			h.responseChans[chainID] <- inboundMessage
		}(inboundMessage, chainID)

	} else {
		inboundMessage.OnFinishedHandling()
	}
}

func (h *RelayerExternalHandler) Connected(nodeID ids.NodeID, version *version.Application, subnetID ids.ID) {
	h.log.Info(
		"connected",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("version", version),
		zap.Stringer("subnetID", subnetID),
	)
}

func (h *RelayerExternalHandler) Disconnected(nodeID ids.NodeID) {
	h.log.Info(
		"disconnected",
		zap.Stringer("nodeID", nodeID),
	)
}

// RegisterRequest registers an AppRequest with the timeout manager.
// If RegisterResponse is not called before the timeout, HandleInbound is called with
// an internally created AppRequestFailed message.
func (h *RelayerExternalHandler) RegisterRequest(reqID ids.RequestID) {
	inMsg := message.InternalAppRequestFailed(
		reqID.NodeID,
		reqID.SourceChainID,
		reqID.RequestID,
	)
	h.timeoutManager.Put(reqID, false, func() {
		h.HandleInbound(context.Background(), inMsg)
	})
}

// RegisterResponse registers an AppResponse with the timeout manager
func (h *RelayerExternalHandler) RegisterResponse(reqID ids.RequestID) {
	h.timeoutManager.Remove(reqID)
}
