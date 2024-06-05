// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"strconv"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Returns true if an error returned by a RelayerDatabase indicates the requested key was not found.
func IsKeyNotFoundError(err error) bool {
	return errors.Is(err, ErrRelayerIDNotFound) || errors.Is(err, ErrKeyNotFound)
}

// Determines the height to process from. There are three cases:
// 1) The database contains the latest processed block data for the chain
//   - In this case, we return the maximum of the latest processed block and the configured processHistoricalBlocksFromHeight
//
// 2) The database has been configured for the chain, but does not contain the latest processed block data
//   - In this case, we return the configured processHistoricalBlocksFromHeight
//
// 3) The database does not contain any information for the chain.
//   - In this case, we return the configured processHistoricalBlocksFromHeight if it is set, otherwise
//     we return the chain head.
func CalculateStartingBlockHeight(
	logger logging.Logger,
	db RelayerDatabase,
	relayerID RelayerID,
	processHistoricalBlocksFromHeight uint64,
	currentHeight uint64,
) (uint64, error) {
	latestProcessedBlock, err := GetLatestProcessedBlockHeight(db, relayerID)
	if IsKeyNotFoundError(err) {
		// The database does not contain the latest processed block data for the chain,
		// use the configured process-historical-blocks-from-height instead.
		// If process-historical-blocks-from-height was not configured, start from the chain head.
		if processHistoricalBlocksFromHeight == 0 {
			return currentHeight, nil
		}
		return processHistoricalBlocksFromHeight, nil
	} else if err != nil {
		// Otherwise, we've encountered an unknown database error
		logger.Error(
			"Failed to get latest block from database",
			zap.String("relayerID", relayerID.ID.String()),
			zap.Error(err),
		)
		return 0, err
	}

	// If the database does contain the latest processed block data for the key,
	// use the max of the latest processed block and the configured start block height (if it was provided)
	if latestProcessedBlock > processHistoricalBlocksFromHeight {
		logger.Info(
			"Processing historical blocks from the latest processed block in the DB",
			zap.String("relayerID", relayerID.ID.String()),
			zap.Uint64("latestProcessedBlock", latestProcessedBlock),
		)
		return latestProcessedBlock, nil
	}
	// Otherwise, return the configured start block height
	logger.Info(
		"Processing historical blocks from the configured start block height",
		zap.String("relayerID", relayerID.ID.String()),
		zap.Uint64("processHistoricalBlocksFromHeight", processHistoricalBlocksFromHeight),
	)
	return processHistoricalBlocksFromHeight, nil
}

// Helper function to get the latest processed block height from the database.
func GetLatestProcessedBlockHeight(db RelayerDatabase, relayerID RelayerID) (uint64, error) {
	latestProcessedBlockData, err := db.Get(relayerID.ID, LatestProcessedBlockKey)
	if err != nil {
		return 0, err
	}
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		return 0, err
	}
	return latestProcessedBlock, nil
}
