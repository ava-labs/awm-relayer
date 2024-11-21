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
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
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
	Message(
		sourceChainID ids.ID,
		requestID uint32,
		timeout time.Duration,
		request []byte,
	) (message.OutboundMessage, error)
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
	lock            *sync.Mutex
	validatorClient *validators.CanonicalValidatorClient
	metrics         *AppRequestNetworkMetrics
	messageCreator  message.Creator

	// Nodes that we should connect to that are not publicly discoverable.
	// Should only be used for local or custom blockchains where validators are not
	// publicly discoverable by primary network nodes.
	manuallyTrackedPeers []info.Peer // TODONOW: we should be able to remove this from the struct
	trackedSubnets       set.Set[ids.ID]
	manager              vdrs.Manager
}

// NewNetwork creates a P2P network client for interacting with validators
func NewNetwork(
	tag string,
	logLevel logging.Level,
	registerer prometheus.Registerer,
	trackedSubnets set.Set[ids.ID],
	messageCreator message.Creator,
	manuallyTrackedPeers []info.Peer,
	cfg Config,
) (AppRequestNetwork, error) {
	logger := logging.NewLogger(
		fmt.Sprintf("p2p-network-%s", tag),
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	metrics, err := newAppRequestNetworkMetrics(registerer)
	if err != nil {
		logger.Fatal("Failed to create app request network metrics", zap.Error(err))
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

	// TODONOW: Get the list of current primary network validators
	validatorClient := validators.NewCanonicalValidatorClient(logger, cfg.GetPChainAPI())
	manager := snowVdrs.NewManager()
	// TODO: We can probably just do this in the below worker
	if err := updatePrimaryNetworkValidators(context.Background(), logger, constants.PrimaryNetworkID, manager, validatorClient); err != nil {
		logger.Error(
			"Failed to set primary network validators on startup",
			zap.Error(err),
		)
		return nil, err
	}

	testNetwork, err := network.NewTestNetwork(logger, networkID, manager, trackedSubnets, handler)
	if err != nil {
		logger.Error(
			"Failed to create test network",
			zap.Error(err),
		)
		return nil, err
	}

	// TODONOW: construct this when we return
	arNetwork := &appRequestNetwork{
		network:              testNetwork,
		handler:              handler,
		infoAPI:              infoAPI,
		logger:               logger,
		lock:                 new(sync.Mutex),
		validatorClient:      validatorClient,
		metrics:              metrics,
		messageCreator:       messageCreator,
		manuallyTrackedPeers: manuallyTrackedPeers,
		trackedSubnets:       trackedSubnets,
		manager:              manager,
	}

	for _, peer := range manuallyTrackedPeers {
		logger.Info("Manually Tracking peer (startup)", zap.String("ID", peer.ID.String()), zap.String("IP", peer.PublicIP.String()))
		arNetwork.network.ManuallyTrack(peer.ID, peer.PublicIP)
	}

	// Connect to a sample of the primary network validators
	// TODO: this seems to be returning subnet A validators as well (but NOT subnet B validators)
	peers, err := infoAPI.Peers(context.Background(), nil)
	if err != nil {
		logger.Error(
			"Failed to get peers",
			zap.Error(err),
		)
		return nil, err
	}
	pClient := platformvm.NewClient(cfg.GetPChainAPI().BaseURL)
	for _, peer := range peers {
		vdrs, err := pClient.GetCurrentValidators(context.Background(), ids.Empty, []ids.NodeID{peer.ID})
		if err != nil {
			panic(err)
		}
		if len(vdrs) == 0 {
			continue
		}
		logger.Info("Manually tracking bootstrap node", zap.String("ID", peer.ID.String()), zap.String("IP", peer.PublicIP.String()))
		arNetwork.network.ManuallyTrack(peer.ID, peer.PublicIP)
	}
	// s := sampler.NewUniform()
	// s.Initialize(uint64(len(peers)))
	// indices, _ := s.Sample(min(len(peers), 5))
	// for _, i := range indices {
	// 	logger.Info("Manually tracking bootstrap node", zap.String("ID", peers[i].ID.String()), zap.String("IP", peers[i].PublicIP.String()))
	// 	arNetwork.network.ManuallyTrack(peers[i].ID, peers[i].PublicIP)
	// }

	// TODO: Is this necessary?
	// Register outselves as a validator (from our view) so that we request all peers
	// manager.AddStaker(constants.PrimaryNetworkID, arNetwork..)

	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	// Periodically update the primary network validator set
	// TODO: This needs to update the subent validator set as well
	// go func() {
	// 	for range time.Tick(5 * time.Second) {
	// 		updatePrimaryNetworkValidators(context.Background(), constants.PrimaryNetworkID, manager, validatorClient)
	// 		for _, subnet := range trackedSubnets.List() {
	// 			updatePrimaryNetworkValidators(context.Background(), subnet, manager, validatorClient)
	// 		}

	// 		// DBG
	// 		connectedPeers := arNetwork.network.PeerInfo(nil)
	// 		arNetwork.logger.Info("Startup Connected peers", zap.Int("count", len(connectedPeers)))
	// 		for _, peer := range connectedPeers {
	// 			arNetwork.logger.Info("Connected peer", zap.String("ID", peer.ID.String()))
	// 		}
	// 	}
	// }()
	arNetwork.startUpdateValidators()

	return arNetwork, nil
}

func (n *appRequestNetwork) TrackSubnet(subnetID ids.ID) {
	n.logger.Debug("acquiring lock 2")
	n.lock.Lock()
	defer n.lock.Unlock()
	n.logger.Debug("acquired lock 2")
	n.logger.Debug("Tracking subnet", zap.String("subnetID", subnetID.String()))
	n.trackedSubnets.Add(subnetID)
}

func (n *appRequestNetwork) startUpdateValidators() {
	go func() {
		for range time.Tick(5 * time.Second) {
			n.logger.Debug("acquiring lock 3")
			n.lock.Lock()
			n.logger.Debug("acquired lock 3")

			updatePrimaryNetworkValidators(context.Background(), n.logger, constants.PrimaryNetworkID, n.manager, n.validatorClient)
			for _, subnet := range n.trackedSubnets.List() {
				updatePrimaryNetworkValidators(context.Background(), n.logger, subnet, n.manager, n.validatorClient)
			}
			n.lock.Unlock()

			// DBG
			connectedPeers := n.network.PeerInfo(nil)
			n.logger.Info("Startup Connected peers", zap.Int("count", len(connectedPeers)))
			for _, peer := range connectedPeers {
				n.logger.Info("Connected peer", zap.String("ID", peer.ID.String()), zap.Any("trackedSubnets", n.trackedSubnets))
			}
		}
	}()
}

func updatePrimaryNetworkValidators(ctx context.Context, logger logging.Logger, subnetID ids.ID, manager vdrs.Manager, client *validators.CanonicalValidatorClient) error {
	logger.Debug("Fetching validators for subnet ID", zap.Stringer("subnetID", subnetID))

	// Fetch the primary network validators from the P-Chain
	height, err := client.GetCurrentHeight(ctx)
	if err != nil {
		return err
	}
	validators, err := client.GetValidatorSet(ctx, height, subnetID)
	if err != nil {
		return err
	}

	validatorsMap := make(map[ids.NodeID]*vdrs.GetValidatorOutput)
	for _, vdr := range validators {
		validatorsMap[vdr.NodeID] = vdr
	}

	// Remove any elements from the manager that are not in the new validator set
	currentVdrs := manager.GetValidatorIDs(subnetID)
	for _, nodeID := range currentVdrs {
		if _, ok := validatorsMap[nodeID]; !ok {
			logger.Debug("Removing validator", zap.Stringer("nodeID", nodeID), zap.Stringer("subnetID", subnetID))
			weight := manager.GetWeight(subnetID, nodeID)
			if err := manager.RemoveWeight(subnetID, nodeID, weight); err != nil {
				return err
			}
		}
	}

	// Add any elements from the new validator set that are not in the manager
	for _, vdr := range validators {
		if _, ok := manager.GetValidator(subnetID, vdr.NodeID); !ok {
			logger.Debug("Adding validator", zap.Stringer("nodeID", vdr.NodeID), zap.Stringer("subnetID", subnetID))
			if err := manager.AddStaker(
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

// ConnectPeers connects the network to peers with the given nodeIDs.
// Returns the set of nodeIDs that were successfully connected to.
func (n *appRequestNetwork) ConnectPeers(nodeIDs set.Set[ids.NodeID]) set.Set[ids.NodeID] {
	n.logger.Debug("acquiring lock 1")
	n.lock.Lock()
	defer n.lock.Unlock()
	n.logger.Debug("acquired lock 1")

	// TODONOW:
	// Get primary network validators
	// Request peers from one of them
	// Iterate through and manually connect to

	// First, check if we are already connected to all the peers
	connectedPeers := n.network.PeerInfo(nil)
	n.logger.Info("Connected peers", zap.Int("count", len(connectedPeers)))
	for _, peer := range connectedPeers {
		n.logger.Info("Connected peer", zap.String("ID", peer.ID.String()))
	}
	// if len(connectedPeers) == nodeIDs.Len() {
	// 	return nodeIDs
	// }

	// If we are not connected to all the peers already, then we have to iterate
	// through the full list of peers obtained from the info API. Rather than iterating
	// through connectedPeers for already tracked peers, just iterate through the full list,
	// re-adding connections to already tracked peers.

	// startInfoAPICall := time.Now()
	// // Get the list of publicly discoverable peers
	// peers, err := n.infoAPI.Peers(context.Background(), nil)
	// n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
	// if err != nil {
	// 	n.logger.Error(
	// 		"Failed to get peers",
	// 		zap.Error(err),
	// 	)
	// 	return nil
	// }
	// n.logger.Info("Fetched peers from info API")
	// for _, peer := range peers {
	// 	n.logger.Info("Peer", zap.String("ID", peer.ID.String()))
	// }

	// // Add manually tracked peers
	// for _, peer := range n.manuallyTrackedPeers {
	// 	n.logger.Info("Manually Tracking peer", zap.String("ID", peer.ID.String()), zap.String("IP", peer.PublicIP.String()))
	// 	n.network.ManuallyTrack(peer.ID, peer.PublicIP)
	// }

	// // Attempt to connect to each peer
	// var trackedNodes set.Set[ids.NodeID]
	// for _, peer := range peers {
	// 	if nodeIDs.Contains(peer.ID) {
	// 		trackedNodes.Add(peer.ID)
	// 		n.logger.Info("Tracking peer", zap.String("ID", peer.ID.String()), zap.String("IP", peer.PublicIP.String()))
	// 		n.network.ManuallyTrack(peer.ID, peer.PublicIP)
	// 		// TODONOW: length checks are not correct
	// 		if len(trackedNodes) == nodeIDs.Len() {
	// 			// DEBUG
	// 			connectedPeers = n.network.PeerInfo(nil)
	// 			n.logger.Info("Connected peers", zap.Int("count", len(connectedPeers)))
	// 			for _, peer := range connectedPeers {
	// 				n.logger.Info("Connected peer", zap.String("ID", peer.ID.String()))
	// 			}
	// 			return trackedNodes
	// 		}
	// 	}
	// }

	// DEBUG
	// connectedPeers = n.network.PeerInfo(nil)
	// n.logger.Info("Connected peers", zap.Int("count", len(connectedPeers)))
	// for _, peer := range connectedPeers {
	// 	n.logger.Info("Connected peer", zap.String("ID", peer.ID.String()))
	// }

	// // If the Info API node is in nodeIDs, it will not be reflected in the call to info.Peers.
	// // In this case, we need to manually track the API node.
	// startInfoAPICall = time.Now()
	// apiNodeID, _, err := n.infoAPI.GetNodeID(context.Background())
	// n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
	// if err != nil {
	// 	n.logger.Error(
	// 		"Failed to get API Node ID",
	// 		zap.Error(err),
	// 	)
	// } else if nodeIDs.Contains(apiNodeID) {
	// 	startInfoAPICall = time.Now()
	// 	apiNodeIPPort, err := n.infoAPI.GetNodeIP(context.Background())
	// 	n.setInfoAPICallLatencyMS(float64(time.Since(startInfoAPICall).Milliseconds()))
	// 	if err != nil {
	// 		n.logger.Error(
	// 			"Failed to get API Node IP",
	// 			zap.Error(err),
	// 		)
	// 	} else {
	// 		trackedNodes.Add(apiNodeID)
	// 		n.network.ManuallyTrack(apiNodeID, apiNodeIPPort)
	// 	}
	// }

	// return trackedNodes
	return nodeIDs
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

	// Manually connect to all peers in the validator set
	// If new peers are connected, AppRequests may fail while the handshake is in progress.
	// In that case, AppRequests to those nodes will be retried in the next iteration of the retry loop.
	n.logger.Debug("connecting to peers")
	connectedNodes := n.ConnectPeers(nodeIDs)
	n.logger.Debug("connected to peers")

	// Calculate the total weight of connected validators.
	connectedWeight := calculateConnectedWeight(validatorSet, nodeValidatorIndexMap, connectedNodes)

	return &ConnectedCanonicalValidators{
		ConnectedWeight:       connectedWeight,
		TotalValidatorWeight:  totalValidatorWeight,
		ValidatorSet:          validatorSet,
		NodeValidatorIndexMap: nodeValidatorIndexMap,
	}, nil
}

func (n *appRequestNetwork) Message(
	sourceChainID ids.ID,
	requestID uint32,
	timeout time.Duration,
	request []byte,
) (message.OutboundMessage, error) {
	return n.messageCreator.AppRequest(
		sourceChainID,
		requestID,
		timeout,
		request,
	)
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
