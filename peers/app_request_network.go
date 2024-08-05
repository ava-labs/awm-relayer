// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	avagoCommon "github.com/ava-labs/avalanchego/snow/engine/common"
	snowVdrs "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers/validators"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	InboundMessageChannelSize = 1000
	DefaultAppRequestTimeout  = time.Second * 2
)

type AppRequestNetwork struct {
	network         network.Network
	handler         *RelayerExternalHandler
	infoAPI         *InfoAPI
	logger          logging.Logger
	lock            *sync.Mutex
	validatorClient *validators.CanonicalValidatorClient
}

// NewNetwork creates a p2p network client for interacting with validators
func NewNetwork(
	logLevel logging.Level,
	registerer prometheus.Registerer,
	trackedSubnets set.Set[ids.ID],
	cfg Config,
) (*AppRequestNetwork, error) {
	logger := logging.NewLogger(
		"awm-relayer-p2p",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	// Create the handler for handling inbound app responses
	handler, err := NewRelayerExternalHandler(logger, registerer)
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

	arNetwork := &AppRequestNetwork{
		network:         testNetwork,
		handler:         handler,
		infoAPI:         infoAPI,
		logger:          logger,
		lock:            new(sync.Mutex),
		validatorClient: validatorClient,
	}
	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	return arNetwork, nil
}

// TODO: remove dependence on Relayer specific config since this is meant to be a generic AppRequestNetwork file
func (n *AppRequestNetwork) InitializeConnectionsAndCheckStake(cfg *config.Config) error {
	// Manually connect to the validators of each of the source subnets.
	// We return an error if we are unable to connect to sufficient stake on any of the subnets.
	// Sufficient stake is determined by the Warp quora of the configured supported destinations,
	// or if the subnet supports all destinations, by the quora of all configured destinations.
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
			if err := n.connectToPrimaryNetworkPeers(cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"Failed to connect to primary network peers: %w",
					err,
				)
			}
		} else {
			if err := n.connectToNonPrimaryNetworkPeers(cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"Failed to connect to non-primary network peers: %w",
					err,
				)
			}
		}
	}
	return nil
}

// ConnectPeers connects the network to peers with the given nodeIDs.
// Returns the set of nodeIDs that were successfully connected to.
func (n *AppRequestNetwork) ConnectPeers(nodeIDs set.Set[ids.NodeID]) set.Set[ids.NodeID] {
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

	// Get the list of peers
	peers, err := n.infoAPI.Peers(context.Background())
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
	if apiNodeID, _, err := n.infoAPI.GetNodeID(context.Background()); err != nil {
		n.logger.Error(
			"Failed to get API Node ID",
			zap.Error(err),
		)
	} else if nodeIDs.Contains(apiNodeID) {
		if apiNodeIPPort, err := n.infoAPI.GetNodeIP(context.Background()); err != nil {
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
	nodeValidatorIndexMap map[ids.NodeID]int
}

// Returns the Warp Validator and its index in the canonical Validator ordering for a given nodeID
func (c *ConnectedCanonicalValidators) GetValidator(nodeID ids.NodeID) (*warp.Validator, int) {
	return c.ValidatorSet[c.nodeValidatorIndexMap[nodeID]], c.nodeValidatorIndexMap[nodeID]
}

// ConnectToCanonicalValidators connects to the canonical validators of the given subnet and returns the connected
// validator information
func (n *AppRequestNetwork) ConnectToCanonicalValidators(subnetID ids.ID) (*ConnectedCanonicalValidators, error) {
	// Get the subnet's current canonical validator set
	validatorSet, totalValidatorWeight, err := n.validatorClient.GetCurrentCanonicalValidatorSet(subnetID)
	if err != nil {
		return nil, err
	}

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
		nodeValidatorIndexMap: nodeValidatorIndexMap,
	}, nil
}

func (n *AppRequestNetwork) Send(
	msg message.OutboundMessage,
	nodeIDs set.Set[ids.NodeID],
	subnetID ids.ID,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	return n.network.Send(msg, avagoCommon.SendConfig{NodeIDs: nodeIDs}, subnetID, allower)
}

func (n *AppRequestNetwork) RegisterAppRequest(requestID ids.RequestID) {
	n.handler.RegisterAppRequest(requestID)
}
func (n *AppRequestNetwork) RegisterRequestID(requestID uint32, numExpectedResponse int) chan message.InboundMessage {
	return n.handler.RegisterRequestID(requestID, numExpectedResponse)
}
func (n *AppRequestNetwork) GetSubnetID(blockchainID ids.ID) (ids.ID, error) {
	return n.validatorClient.GetSubnetID(context.Background(), blockchainID)
}

// Private helpers

// Connect to the validators of the source blockchain. For each destination blockchain,
// verify that we have connected to a threshold of stake.
func (n *AppRequestNetwork) connectToNonPrimaryNetworkPeers(
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	subnetID := sourceBlockchain.GetSubnetID()
	connectedValidators, err := n.ConnectToCanonicalValidators(subnetID)
	if err != nil {
		n.logger.Error(
			"Failed to connect to canonical validators",
			zap.String("subnetID", subnetID.String()),
			zap.Error(err),
		)
		return err
	}
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		if ok, quorum, err := n.checkForSufficientConnectedStake(cfg, connectedValidators, blockchainID); !ok {
			n.logger.Error(
				"Failed to connect to a threshold of stake",
				zap.String("destinationBlockchainID", blockchainID.String()),
				zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
				zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
				zap.Any("warpQuorum", quorum),
			)
			return err
		}
	}
	return nil
}

// Connect to the validators of the destination blockchains. Verify that we have connected
// to a threshold of stake for each blockchain.
func (n *AppRequestNetwork) connectToPrimaryNetworkPeers(
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		subnetID := cfg.GetSubnetID(blockchainID)
		connectedValidators, err := n.ConnectToCanonicalValidators(subnetID)
		if err != nil {
			n.logger.Error(
				"Failed to connect to canonical validators",
				zap.String("subnetID", subnetID.String()),
				zap.Error(err),
			)
			return err
		}

		if ok, quorum, err := n.checkForSufficientConnectedStake(cfg, connectedValidators, blockchainID); !ok {
			n.logger.Error(
				"Failed to connect to a threshold of stake",
				zap.String("destinationBlockchainID", blockchainID.String()),
				zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
				zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
				zap.Any("warpQuorum", quorum),
			)
			return err
		}
	}
	return nil
}

// Fetch the warp quorum from the config and check if the connected stake exceeds the threshold
func (n *AppRequestNetwork) checkForSufficientConnectedStake(
	cfg *config.Config,
	connectedValidators *ConnectedCanonicalValidators,
	destinationBlockchainID ids.ID,
) (bool, *config.WarpQuorum, error) {
	quorum, err := cfg.GetWarpQuorum(destinationBlockchainID)
	if err != nil {
		n.logger.Error(
			"Failed to get warp quorum from config",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.Error(err),
		)
		return false, nil, err
	}
	return utils.CheckStakeWeightExceedsThreshold(
		big.NewInt(0).SetUint64(connectedValidators.ConnectedWeight),
		connectedValidators.TotalValidatorWeight,
		quorum.QuorumNumerator,
		quorum.QuorumDenominator,
	), &quorum, nil
}
