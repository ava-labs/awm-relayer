package checkpoint

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/relayer"
	"go.uber.org/zap"
)

// Determines the height to process from. There are three cases:
// 1) The database contains the latest processed block data for the chain
//   - In this case, we return the maximum of the latest processed block and the
//     configured processHistoricalBlocksFromHeight.
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
	relayerID relayer.RelayerID,
	processHistoricalBlocksFromHeight uint64,
	currentHeight uint64,
) (uint64, error) {
	latestProcessedBlock, err := db.GetLatestProcessedBlockHeight(relayerID)
	if errors.Is(err, ErrNotFound) {
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