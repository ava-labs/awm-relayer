// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"go.uber.org/zap"
)

// Convenience function to initialize connections and check stake for all source blockchains.
// Only returns an error if it fails to get a list of canonical validator or a valid warp config.
//
// Failing a sufficient stake check will only log an error but still return successfully
// since each attempted relay will make an attempt at reconnecting to any missing validators.
//
// Sufficient stake is determined by the Warp quora of the configured supported destinations,
// or if the subnet supports all destinations, by the quora of all configured destinations.
func InitializeConnectionsAndCheckStake(
	logger logging.Logger,
	network peers.AppRequestNetwork,
	cfg *config.Config,
) error {
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
			if err := connectToPrimaryNetworkPeers(logger, network, cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"failed to connect to primary network peers: %w",
					err,
				)
			}
		} else {
			if err := connectToNonPrimaryNetworkPeers(logger, network, cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"failed to connect to non-primary network peers: %w",
					err,
				)
			}
		}
	}
	return nil
}

// Connect to the validators of the source blockchain. For each destination blockchain,
// verify that we have connected to a threshold of stake.
func connectToNonPrimaryNetworkPeers(
	logger logging.Logger,
	network peers.AppRequestNetwork,
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	subnetID := sourceBlockchain.GetSubnetID()
	connectedValidators, err := network.ConnectToCanonicalValidators(0, subnetID)
	if err != nil {
		logger.Error(
			"Failed to connect to canonical validators",
			zap.String("subnetID", subnetID.String()),
			zap.Error(err),
		)
		return err
	}
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		if ok, quorum, err := checkForSufficientConnectedStake(logger, cfg, connectedValidators, blockchainID); !ok {
			logger.Warn(
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
func connectToPrimaryNetworkPeers(
	logger logging.Logger,
	network peers.AppRequestNetwork,
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		subnetID := cfg.GetSubnetID(blockchainID)
		connectedValidators, err := network.ConnectToCanonicalValidators(0, subnetID)
		if err != nil {
			logger.Error(
				"Failed to connect to canonical validators",
				zap.String("subnetID", subnetID.String()),
				zap.Error(err),
			)
			return err
		}

		if ok, quorum, err := checkForSufficientConnectedStake(logger, cfg, connectedValidators, blockchainID); !ok {
			logger.Warn(
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
func checkForSufficientConnectedStake(
	logger logging.Logger,
	cfg *config.Config,
	connectedValidators *peers.ConnectedCanonicalValidators,
	destinationBlockchainID ids.ID,
) (bool, *config.WarpQuorum, error) {
	quorum, err := cfg.GetWarpQuorum(destinationBlockchainID)
	if err != nil {
		logger.Error(
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
