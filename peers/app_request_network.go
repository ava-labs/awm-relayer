// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_app_request_network.go -package=mocks

package peers

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	avagoCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	snowVdrs "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/peers/validators"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	InboundMessageChannelSize = 1000
	DefaultAppRequestTimeout  = time.Second * 2
)

type AppRequestNetwork interface {
	ConnectPeers(nodeIDs set.Set[ids.NodeID]) set.Set[ids.NodeID]
	ConnectToCanonicalValidators(subnetID ids.ID, height uint64) (
		*ConnectedCanonicalValidators,
		error,
	)
	GetSubnetID(blockchainID ids.ID) (ids.ID, error)
	RegisterAppRequest(requestID ids.RequestID)
	RegisterRequestID(
		requestID uint32,
		numExpectedResponse int,
	) chan message.InboundMessage
	Send(
		msg message.OutboundMessage,
		nodeIDs set.Set[ids.NodeID],
		subnetID ids.ID,
		allower subnets.Allower,
	) set.Set[ids.NodeID]
}

type appRequestNetwork struct {
	network         network.Network
	handler         *RelayerExternalHandler
	infoAPI         *InfoAPI
	logger          logging.Logger
	lock            *sync.Mutex
	validatorClient *validators.CanonicalValidatorClient
	metrics         *AppRequestNetworkMetrics
}

// NewNetwork creates a p2p network client for interacting with validators
func NewNetwork(
	logLevel logging.Level,
	registerer prometheus.Registerer,
	trackedSubnets set.Set[ids.ID],
	cfg Config,
) (AppRequestNetwork, error) {
	logger := logging.NewLogger(
		"p2p-network",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	metrics, err := newAppRequestNetworkMetrics(registerer)
	if err != nil {
		logger.Fatal("Failed to create app request network metrics", zap.Error(err))
		panic(err)
	}

	// Create the handler for handling inbound app responses
	handler, err := NewRelayerExternalHandler(logger, metrics)
	if err != nil {
		logger.Error(
			"Failed to create p2p network handler",
			zap.Error(err),
		)
		return nil, err
	}

	infoAPI, err := NewInfoAPI(cfg.GetInfoAPI())
	if err != nil {
		logger.Error(
			"Failed to create info API",
			zap.Error(err),
		)
		return nil, err
	}
	networkID, err := infoAPI.GetNetworkID(context.Background())
	if err != nil {
		logger.Error(
			"Failed to get network ID",
			zap.Error(err),
		)
		return nil, err
	}

	testNetwork, err := network.NewTestNetwork(logger, networkID, snowVdrs.NewManager(), trackedSubnets, handler)
	if err != nil {
		logger.Error(
			"Failed to create test network",
			zap.Error(err),
		)
		return nil, err
	}

	validatorClient := validators.NewCanonicalValidatorClient(logger, cfg.GetPChainAPI())

	arNetwork := &appRequestNetwork{
		network:         testNetwork,
		handler:         handler,
		infoAPI:         infoAPI,
		logger:          logger,
		lock:            new(sync.Mutex),
		validatorClient: validatorClient,
		metrics:         metrics,
	}
	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	return arNetwork, nil
}

// ConnectPeers connects the network to peers with the given nodeIDs.
// Returns the set of nodeIDs that were successfully connected to.
func (n *appRequestNetwork) ConnectPeers(nodeIDs set.Set[ids.NodeID]) set.Set[ids.NodeID] {
	n.lock.Lock()
	defer n.lock.Unlock()

	// First, check if we are already connected to all the peers
	connectedPeers := n.network.PeerInfo(nodeIDs.List())
	if len(connectedPeers) == nodeIDs.Len() {
		return nodeIDs
	}

	// If we are not connected to all the peers already, then we have to iterate
	// through the full list of peers obtained from the info API. Rather than iterating
	// through connectedPeers for already tracked peers, just iterate through the full list,
	// re-adding connections to already tracked peers.

	startInfoAPICall := time.Now()
	// Get the list of peers
	peers, err := n.infoAPI.Peers(context.Background())
	n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
	if err != nil {
		n.logger.Error(
			"Failed to get peers",
			zap.Error(err),
		)
		return nil
	}

	// Attempt to connect to each peer
	var trackedNodes set.Set[ids.NodeID]
	for _, peer := range peers {
		if nodeIDs.Contains(peer.ID) {
			trackedNodes.Add(peer.ID)
			n.network.ManuallyTrack(peer.ID, peer.PublicIP)
			if len(trackedNodes) == nodeIDs.Len() {
				return trackedNodes
			}
		}
	}

	// If the Info API node is in nodeIDs, it will not be reflected in the call to info.Peers.
	// In this case, we need to manually track the API node.
	startInfoAPICall = time.Now()
	apiNodeID, _, err := n.infoAPI.GetNodeID(context.Background())
	n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
	if err != nil {
		n.logger.Error(
			"Failed to get API Node ID",
			zap.Error(err),
		)
	} else if nodeIDs.Contains(apiNodeID) {
		startInfoAPICall = time.Now()
		apiNodeIPPort, err := n.infoAPI.GetNodeIP(context.Background())
		n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
		if err != nil {
			n.logger.Error(
				"Failed to get API Node IP",
				zap.Error(err),
			)
		} else {
			trackedNodes.Add(apiNodeID)
			n.network.ManuallyTrack(apiNodeID, apiNodeIPPort)
		}
	}

	return trackedNodes
}

// Helper struct to hold connected validator information
// Warp Validators sharing the same BLS key may consist of multiple nodes,
// so we need to track the node ID to validator index mapping
type ConnectedCanonicalValidators struct {
	ConnectedWeight       uint64
	TotalValidatorWeight  uint64
	ValidatorSet          []*warp.Validator
	NodeValidatorIndexMap map[ids.NodeID]int
}

// Returns the Warp Validator and its index in the canonical Validator ordering for a given nodeID
func (c *ConnectedCanonicalValidators) GetValidator(nodeID ids.NodeID) (*warp.Validator, int) {
	return c.ValidatorSet[c.NodeValidatorIndexMap[nodeID]], c.NodeValidatorIndexMap[nodeID]
}

// ConnectToCanonicalValidators connects to the canonical validators of the given subnet and returns the connected
// validator information
func (n *appRequestNetwork) ConnectToCanonicalValidators(
	subnetID ids.ID,
	height uint64,
) (*ConnectedCanonicalValidators, error) {
	var validatorSet []*warp.Validator
	var totalValidatorWeight uint64
	var err error

	startPChainAPICall := time.Now()
	if height == 0 {
		// Get the subnet's current canonical validator set
		validatorSet, totalValidatorWeight, err = n.validatorClient.GetCurrentCanonicalValidatorSet(subnetID)
		if err != nil {
			return nil, err
		}
	} else {
		// Get the subnet's canonical validator set at the given height
		vdrs, err := n.validatorClient.GetValidatorSet(context.Background(), height, subnetID)
		if err != nil {
			return nil, err
		}
		validatorSet, totalValidatorWeight, err = warp.FlattenValidatorSet(vdrs)
		if err != nil {
			return nil, err
		}
	}
	n.setPChainAPICallLatencyMS(float64(time.Since(startPChainAPICall).Milliseconds()))

	// We make queries to node IDs, not unique validators as represented by a BLS pubkey, so we need this map to track
	// responses from nodes and populate the signatureMap with the corresponding validator signature
	// This maps node IDs to the index in the canonical validator set
	nodeValidatorIndexMap := make(map[ids.NodeID]int)
	for i, vdr := range validatorSet {
		for _, node := range vdr.NodeIDs {
			nodeValidatorIndexMap[node] = i
		}
	}

	// Manually connect to all peers in the validator set
	// If new peers are connected, AppRequests may fail while the handshake is in progress.
	// In that case, AppRequests to those nodes will be retried in the next iteration of the retry loop.
	nodeIDs := set.NewSet[ids.NodeID](len(nodeValidatorIndexMap))
	for node := range nodeValidatorIndexMap {
		nodeIDs.Add(node)
	}
	connectedNodes := n.ConnectPeers(nodeIDs)

	// Check if we've connected to a stake threshold of nodes
	connectedWeight := uint64(0)
	for node := range connectedNodes {
		connectedWeight += validatorSet[nodeValidatorIndexMap[node]].Weight
	}
	return &ConnectedCanonicalValidators{
		ConnectedWeight:       connectedWeight,
		TotalValidatorWeight:  totalValidatorWeight,
		ValidatorSet:          validatorSet,
		NodeValidatorIndexMap: nodeValidatorIndexMap,
	}, nil
}

func (n *appRequestNetwork) Send(
	msg message.OutboundMessage,
	nodeIDs set.Set[ids.NodeID],
	subnetID ids.ID,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	return n.network.Send(msg, avagoCommon.SendConfig{NodeIDs: nodeIDs}, subnetID, allower)
}

func (n *appRequestNetwork) RegisterAppRequest(requestID ids.RequestID) {
	n.handler.RegisterAppRequest(requestID)
}
func (n *appRequestNetwork) RegisterRequestID(requestID uint32, numExpectedResponse int) chan message.InboundMessage {
	return n.handler.RegisterRequestID(requestID, numExpectedResponse)
}
func (n *appRequestNetwork) GetSubnetID(blockchainID ids.ID) (ids.ID, error) {
	return n.validatorClient.GetSubnetID(context.Background(), blockchainID)
}

//
// Metrics
//

func (n *appRequestNetwork) setInfoAPICallLatencyMS(latency float64) {
	n.metrics.infoAPICallLatencyMS.Observe(latency)
}

func (n *appRequestNetwork) setPChainAPICallLatencyMS(latency float64) {
	n.metrics.pChainAPICallLatencyMS.Observe(latency)
}
