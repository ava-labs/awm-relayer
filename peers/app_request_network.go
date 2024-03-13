// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"errors"
	"math/big"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	snowVdrs "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/validators"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	InboundMessageChannelSize = 1000
	DefaultAppRequestTimeout  = time.Second * 2

	numInitialTestPeers = 5
)

type AppRequestNetwork struct {
	Network    network.Network
	Handler    *RelayerExternalHandler
	infoClient info.Client
	logger     logging.Logger
	lock       *sync.Mutex
}

// NewNetwork connects to a peers at the app request level.
func NewNetwork(
	logLevel logging.Level,
	registerer prometheus.Registerer,
	cfg *config.Config,
	infoClient info.Client,
	pChainClient platformvm.Client,
) (*AppRequestNetwork, map[ids.ID]chan message.InboundMessage, error) {
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
		return nil, nil, err
	}

	// Create the test network for AppRequests
	var trackedSubnets set.Set[ids.ID]
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		trackedSubnets.Add(sourceBlockchain.GetSubnetID())
	}

	// Construct a response chan for each chain. Inbound messages will be routed to the proper channel in the handler
	responseChans := make(map[ids.ID]chan message.InboundMessage)
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		responseChan := make(chan message.InboundMessage, InboundMessageChannelSize)
		responseChans[sourceBlockchain.GetBlockchainID()] = responseChan
	}
	responseChansLock := new(sync.RWMutex)

	handler, err := NewRelayerExternalHandler(logger, registerer, responseChans, responseChansLock)
	if err != nil {
		logger.Error(
			"Failed to create p2p network handler",
			zap.Error(err),
		)
		return nil, nil, err
	}

	testNetwork, err := network.NewTestNetwork(logger, networkID, snowVdrs.NewManager(), trackedSubnets, handler)
	if err != nil {
		logger.Error(
			"Failed to create test network",
			zap.Error(err),
		)
		return nil, nil, err
	}

	// We need to initially connect to some nodes in the network before peer
	// gossip will enable connecting to all the remaining nodes in the network.
	var beaconIPs, beaconIDs []string

	peers, err := infoClient.Peers(context.Background())
	if err != nil {
		logger.Error(
			"Failed to get peers",
			zap.Error(err),
		)
		return nil, nil, err
	}

	// Randomly select peers to connect to until we have numInitialTestPeers
	indices := rand.Perm(len(peers))
	for _, index := range indices {
		// Do not attempt to connect to private peers
		if len(peers[index].PublicIP) == 0 {
			continue
		}
		beaconIPs = append(beaconIPs, peers[index].PublicIP)
		beaconIDs = append(beaconIDs, peers[index].ID.String())
		if len(beaconIDs) == numInitialTestPeers {
			break
		}
	}
	if len(beaconIPs) == 0 {
		logger.Error(
			"Failed to find any peers to connect to",
			zap.Error(err),
		)
		return nil, nil, err
	}
	if len(beaconIPs) < numInitialTestPeers {
		logger.Warn(
			"Failed to find a full set of peers to connect to on startup",
			zap.Int("connectedPeers", len(beaconIPs)),
			zap.Int("expectedConnectedPeers", numInitialTestPeers),
		)
	}

	for i, beaconIDStr := range beaconIDs {
		beaconID, err := ids.NodeIDFromString(beaconIDStr)
		if err != nil {
			logger.Error(
				"Failed to parse beaconID",
				zap.String("beaconID", beaconIDStr),
				zap.Error(err),
			)
			return nil, nil, err
		}

		beaconIPStr := beaconIPs[i]
		ipPort, err := ips.ToIPPort(beaconIPStr)
		if err != nil {
			logger.Error(
				"Failed to parse beaconIP",
				zap.String("beaconIP", beaconIPStr),
				zap.Error(err),
			)
			return nil, nil, err
		}

		testNetwork.ManuallyTrack(beaconID, ipPort)
	}

	arNetwork := &AppRequestNetwork{
		Network:    testNetwork,
		Handler:    handler,
		infoClient: infoClient,
		logger:     logger,
		lock:       new(sync.Mutex),
	}

	// Manually connect to the validators of each of the source subnets.
	// We return an error if we are unable to connect to sufficient stake on any of the subnets.
	// Sufficient stake is determined by the Warp quora of the configured supported destinations,
	// or if the subnet supports all destinations, by the quora of all configured destinations.
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		connectedWeight, totalValidatorWeight, _, _, err := ConnectToCanonicalValidators(
			arNetwork,
			validators.NewCanonicalValidatorClient(logger, pChainClient),
			sourceBlockchain.GetSubnetID(),
		)
		if err != nil {
			logger.Error(
				"Failed to connect to canonical validators",
				zap.Error(err),
			)
			return nil, nil, err
		}

		var destinationBlockchainIDs []ids.ID
		if supportedDsts := sourceBlockchain.GetSupportedDestinations(); supportedDsts.Len() > 0 {
			destinationBlockchainIDs = supportedDsts.List()
		} else {
			for _, dst := range cfg.DestinationBlockchains {
				destinationBlockchainIDs = append(destinationBlockchainIDs, dst.GetBlockchainID())
			}
		}

		for _, destinationBlockchainID := range destinationBlockchainIDs {
			quorum, err := cfg.GetWarpQuorum(destinationBlockchainID)
			if err != nil {
				logger.Error(
					"Failed to get warp quorum from config",
					zap.Error(err),
				)
				return nil, nil, err
			}
			if !utils.CheckStakeWeightExceedsThreshold(
				big.NewInt(0).SetUint64(connectedWeight),
				totalValidatorWeight,
				quorum.QuorumNumerator,
				quorum.QuorumDenominator,
			) {
				logger.Error(
					"Failed to connect to a threshold of stake",
					zap.Uint64("connectedWeight", connectedWeight),
					zap.Uint64("totalValidatorWeight", totalValidatorWeight),
					zap.Any("warpQuorum", quorum),
				)
				return nil, nil, errors.New("failed to connect to a threshold of stake")
			}
		}

	}

	go logger.RecoverAndPanic(func() {
		testNetwork.Dispatch()
	})

	return arNetwork, responseChans, nil
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
