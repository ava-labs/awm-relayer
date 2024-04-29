// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	snowVdrs "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/validators"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	InboundMessageChannelSize = 1000
	DefaultAppRequestTimeout  = time.Second * 2
)

type AppRequestNetwork struct {
	Network         network.Network
	Handler         *RelayerExternalHandler
	infoClient      info.Client
	logger          logging.Logger
	lock            *sync.Mutex
	validatorClient *validators.CanonicalValidatorClient
}

// NewNetwork connects to a peers at the app request level.
func NewNetwork(
	logLevel logging.Level,
	registerer prometheus.Registerer,
	cfg *config.Config,
	infoClient info.Client,
	pChainClient platformvm.Client,
) (*AppRequestNetwork, error) {
	logger := logging.NewLogger(
		"awm-relayer-p2p",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	networkID, err := infoClient.GetNetworkID(context.Background())
	if err != nil {
		logger.Error(
			"Failed to get network ID",
			zap.Error(err),
		)
		return nil, err
	}

	// Create the test network for AppRequests
	var trackedSubnets set.Set[ids.ID]
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		trackedSubnets.Add(sourceBlockchain.GetSubnetID())
	}

	handler, err := NewRelayerExternalHandler(logger, registerer)
	if err != nil {
		logger.Error(
			"Failed to create p2p network handler",
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

	validatorClient := validators.NewCanonicalValidatorClient(logger, pChainClient)

	arNetwork := &AppRequestNetwork{
		Network:         testNetwork,
		Handler:         handler,
		infoClient:      infoClient,
		logger:          logger,
		lock:            new(sync.Mutex),
		validatorClient: validatorClient,
	}

	// Manually connect to the validators of each of the source subnets.
	// We return an error if we are unable to connect to sufficient stake on any of the subnets.
	// Sufficient stake is determined by the Warp quora of the configured supported destinations,
	// or if the subnet supports all destinations, by the quora of all configured destinations.
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
			if err := arNetwork.connectToPrimaryNetworkPeers(cfg, sourceBlockchain); err != nil {
				return nil, err
			}
		} else {
			if err := arNetwork.connectToNonPrimaryNetworkPeers(cfg, sourceBlockchain); err != nil {
				return nil, err
			}
		}
	}

	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	return arNetwork, nil
}

// ConnectPeers connects the network to peers with the given nodeIDs.
// Returns the set of nodeIDs that were successfully connected to.
func (n *AppRequestNetwork) ConnectPeers(nodeIDs set.Set[ids.NodeID]) set.Set[ids.NodeID] {
	n.lock.Lock()
	defer n.lock.Unlock()

	// First, check if we are already connected to all the peers
	connectedPeers := n.Network.PeerInfo(nodeIDs.List())
	if len(connectedPeers) == nodeIDs.Len() {
		return nodeIDs
	}

	// If we are not connected to all the peers already, then we have to iterate
	// through the full list of peers obtained from the info API. Rather than iterating
	// through connectedPeers for already tracked peers, just iterate through the full list,
	// re-adding connections to already tracked peers.

	// Get the list of peers
	peers, err := n.infoClient.Peers(context.Background())
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
			ipPort, err := ips.ToIPPort(peer.PublicIP)
			if err != nil {
				n.logger.Error(
					"Failed to parse peer IP",
					zap.String("beaconIP", peer.PublicIP),
					zap.Error(err),
				)
				continue
			}
			trackedNodes.Add(peer.ID)
			n.Network.ManuallyTrack(peer.ID, ipPort)
			if len(trackedNodes) == nodeIDs.Len() {
				return trackedNodes
			}
		}
	}

	// If the Info API node is in nodeIDs, it will not be reflected in the call to info.Peers.
	// In this case, we need to manually track the API node.
	if apiNodeID, _, err := n.infoClient.GetNodeID(context.Background()); err != nil {
		n.logger.Error(
			"Failed to get API Node ID",
			zap.Error(err),
		)
	} else if nodeIDs.Contains(apiNodeID) {
		if apiNodeIP, err := n.infoClient.GetNodeIP(context.Background()); err != nil {
			n.logger.Error(
				"Failed to get API Node IP",
				zap.Error(err),
			)
		} else if ipPort, err := ips.ToIPPort(apiNodeIP); err != nil {
			n.logger.Error(
				"Failed to parse API Node IP",
				zap.String("nodeIP", apiNodeIP),
				zap.Error(err),
			)
		} else {
			trackedNodes.Add(apiNodeID)
			n.Network.ManuallyTrack(apiNodeID, ipPort)
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

// Private helpers

// Connect to the validators of the source blockchain. For each destination blockchain, verify that we have connected to a threshold of stake.
func (n *AppRequestNetwork) connectToNonPrimaryNetworkPeers(cfg *config.Config, sourceBlockchain *config.SourceBlockchain) error {
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

// Connect to the validators of the destination blockchains. Verify that we have connected to a threshold of stake for each blockchain.
func (n *AppRequestNetwork) connectToPrimaryNetworkPeers(cfg *config.Config, sourceBlockchain *config.SourceBlockchain) error {
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
