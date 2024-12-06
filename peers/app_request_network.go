// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_app_request_network.go -package=mocks

package peers

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	avagoCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	snowVdrs "github.com/ava-labs/avalanchego/snow/validators"
	vdrs "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/icm-services/peers/utils"
	"github.com/ava-labs/icm-services/peers/validators"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	InboundMessageChannelSize = 1000
	DefaultAppRequestTimeout  = time.Second * 2
	ValidatorRefreshPeriod    = time.Second * 5
	NumBootstrapNodes         = 5
)

type AppRequestNetwork interface {
	ConnectToCanonicalValidators(subnetID ids.ID) (
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
	Shutdown()
	TrackSubnet(subnetID ids.ID)
}

type appRequestNetwork struct {
	network         network.Network
	handler         *RelayerExternalHandler
	infoAPI         *InfoAPI
	logger          logging.Logger
	lock            *sync.RWMutex
	validatorClient *validators.CanonicalValidatorClient
	metrics         *AppRequestNetworkMetrics

	trackedSubnets set.Set[ids.ID]
	manager        vdrs.Manager
}

// NewNetwork creates a P2P network client for interacting with validators
func NewNetwork(
	logLevel logging.Level,
	registerer prometheus.Registerer,
	trackedSubnets set.Set[ids.ID],
	manuallyTrackedPeers []info.Peer,
	cfg Config,
	allowPrivateIPs bool,
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
		logger.Error("Failed to create app request network metrics", zap.Error(err))
		return nil, err
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

	validatorClient := validators.NewCanonicalValidatorClient(logger, cfg.GetPChainAPI())
	manager := snowVdrs.NewManager()

	testNetworkRegisterer := prometheus.NewRegistry()

	testNetworkConfig, err := network.NewTestNetworkConfig(testNetworkRegisterer, networkID, manager, trackedSubnets)
	if err != nil {
		logger.Error(
			"Failed to create test network config",
			zap.Error(err),
		)
		return nil, err
	}
	testNetworkConfig.AllowPrivateIPs = allowPrivateIPs

	testNetwork, err := network.NewTestNetwork(logger, testNetworkRegisterer, testNetworkConfig, handler)
	if err != nil {
		logger.Error(
			"Failed to create test network",
			zap.Error(err),
		)
		return nil, err
	}

	for _, peer := range manuallyTrackedPeers {
		logger.Info(
			"Manually Tracking peer (startup)",
			zap.Stringer("ID", peer.ID),
			zap.Stringer("IP", peer.PublicIP),
		)
		testNetwork.ManuallyTrack(peer.ID, peer.PublicIP)
	}

	// Connect to a sample of the primary network validators, with connection
	// info pulled from the info API
	peers, err := infoAPI.Peers(context.Background(), nil)
	if err != nil {
		logger.Error(
			"Failed to get peers",
			zap.Error(err),
		)
		return nil, err
	}
	peersMap := make(map[ids.NodeID]info.Peer)
	for _, peer := range peers {
		peersMap[peer.ID] = peer
	}

	pClient := platformvm.NewClient(cfg.GetPChainAPI().BaseURL)
	options := utils.InitializeOptions(cfg.GetPChainAPI())
	vdrs, err := pClient.GetCurrentValidators(context.Background(), constants.PrimaryNetworkID, nil, options...)
	if err != nil {
		logger.Error("Failed to get current validators", zap.Error(err))
		return nil, err
	}

	// Sample until we've connected to the target number of bootstrap nodes
	s := sampler.NewUniform()
	s.Initialize(uint64(len(vdrs)))
	numConnected := 0
	for numConnected < NumBootstrapNodes {
		i, ok := s.Next()
		if !ok {
			// If we've sampled all the nodes and still haven't connected to the target number of bootstrap nodes,
			// then warn and stop sampling by either returning an error or breaking
			logger.Warn(
				"Failed to connect to enough bootstrap nodes",
				zap.Int("targetBootstrapNodes", NumBootstrapNodes),
				zap.Int("numAvailablePeers", len(peers)),
				zap.Int("connectedBootstrapNodes", numConnected),
			)
			if numConnected == 0 {
				return nil, fmt.Errorf("failed to connect to any bootstrap nodes")
			}
			break
		}
		if peer, ok := peersMap[vdrs[i].NodeID]; ok {
			logger.Info(
				"Manually tracking bootstrap node",
				zap.Stringer("ID", peer.ID),
				zap.Stringer("IP", peer.PublicIP),
			)
			testNetwork.ManuallyTrack(peer.ID, peer.PublicIP)
			numConnected++
		}
	}

	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	arNetwork := &appRequestNetwork{
		network:         testNetwork,
		handler:         handler,
		infoAPI:         infoAPI,
		logger:          logger,
		lock:            new(sync.RWMutex),
		validatorClient: validatorClient,
		metrics:         metrics,
		trackedSubnets:  trackedSubnets,
		manager:         manager,
	}

	arNetwork.startUpdateValidators()

	return arNetwork, nil
}

// Helper to scope read lock acquisition
func (n *appRequestNetwork) containsSubnet(subnetID ids.ID) bool {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.trackedSubnets.Contains(subnetID)
}

func (n *appRequestNetwork) TrackSubnet(subnetID ids.ID) {
	if n.containsSubnet(subnetID) {
		return
	}

	n.logger.Debug("Tracking subnet", zap.Stringer("subnetID", subnetID))
	n.trackedSubnets.Add(subnetID)
	n.updateValidatorSet(context.Background(), subnetID)
}

func (n *appRequestNetwork) startUpdateValidators() {
	go func() {
		// Fetch validators immediately when called, and refresh every ValidatorRefreshPeriod
		ticker := time.NewTicker(ValidatorRefreshPeriod)
		for ; true; <-ticker.C {
			n.updateValidatorSet(context.Background(), constants.PrimaryNetworkID)
			for _, subnet := range n.trackedSubnets.List() {
				n.updateValidatorSet(context.Background(), subnet)
			}
		}
	}()
}

func (n *appRequestNetwork) updateValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.logger.Debug("Fetching validators for subnet ID", zap.Stringer("subnetID", subnetID))

	// Fetch the subnet validators from the P-Chain
	validators, err := n.validatorClient.GetProposedValidators(ctx, subnetID)
	if err != nil {
		return err
	}

	validatorsMap := make(map[ids.NodeID]*vdrs.GetValidatorOutput)
	for _, vdr := range validators {
		validatorsMap[vdr.NodeID] = vdr
	}

	// Remove any elements from the manager that are not in the new validator set
	currentVdrs := n.manager.GetValidatorIDs(subnetID)
	for _, nodeID := range currentVdrs {
		if _, ok := validatorsMap[nodeID]; !ok {
			n.logger.Debug("Removing validator", zap.Stringer("nodeID", nodeID), zap.Stringer("subnetID", subnetID))
			weight := n.manager.GetWeight(subnetID, nodeID)
			if err := n.manager.RemoveWeight(subnetID, nodeID, weight); err != nil {
				return err
			}
		}
	}

	// Add any elements from the new validator set that are not in the manager
	for _, vdr := range validators {
		if _, ok := n.manager.GetValidator(subnetID, vdr.NodeID); !ok {
			n.logger.Debug("Adding validator", zap.Stringer("nodeID", vdr.NodeID), zap.Stringer("subnetID", subnetID))
			if err := n.manager.AddStaker(
				subnetID,
				vdr.NodeID,
				vdr.PublicKey,
				ids.Empty,
				vdr.Weight,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *appRequestNetwork) Shutdown() {
	n.network.StartClose()
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
func (n *appRequestNetwork) ConnectToCanonicalValidators(subnetID ids.ID) (*ConnectedCanonicalValidators, error) {
	// Track the subnet
	n.TrackSubnet(subnetID)

	// Get the subnet's current canonical validator set
	startPChainAPICall := time.Now()
	validatorSet, totalValidatorWeight, err := n.validatorClient.GetCurrentCanonicalValidatorSet(subnetID)
	n.setPChainAPICallLatencyMS(float64(time.Since(startPChainAPICall).Milliseconds()))
	if err != nil {
		return nil, err
	}

	// We make queries to node IDs, not unique validators as represented by a BLS pubkey, so we need this map to track
	// responses from nodes and populate the signatureMap with the corresponding validator signature
	// This maps node IDs to the index in the canonical validator set
	nodeValidatorIndexMap := make(map[ids.NodeID]int)
	nodeIDs := set.NewSet[ids.NodeID](len(nodeValidatorIndexMap))
	for i, vdr := range validatorSet {
		for _, node := range vdr.NodeIDs {
			nodeValidatorIndexMap[node] = i
			nodeIDs.Add(node)
		}
	}

	// Calculate the total weight of connected validators.
	connectedWeight := calculateConnectedWeight(validatorSet, nodeValidatorIndexMap, nodeIDs)

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

func (n *appRequestNetwork) setPChainAPICallLatencyMS(latency float64) {
	n.metrics.pChainAPICallLatencyMS.Observe(latency)
}

// Non-receiver util functions

func calculateConnectedWeight(
	validatorSet []*warp.Validator,
	nodeValidatorIndexMap map[ids.NodeID]int,
	connectedNodes set.Set[ids.NodeID],
) uint64 {
	connectedBLSPubKeys := set.NewSet[string](len(validatorSet))
	connectedWeight := uint64(0)
	for node := range connectedNodes {
		vdr := validatorSet[nodeValidatorIndexMap[node]]
		blsPubKey := hex.EncodeToString(vdr.PublicKeyBytes)
		if connectedBLSPubKeys.Contains(blsPubKey) {
			continue
		}
		connectedWeight += vdr.Weight
		connectedBLSPubKeys.Add(blsPubKey)
	}
	return connectedWeight
}
