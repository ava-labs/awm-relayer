// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"strconv"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

//
// DatabaseManager writes all committed keys to the database on a timer.
//

type DatabaseManager struct {
	logger               logging.Logger
	committedHeights     map[RelayerID]uint64
	committedHeightsLock *sync.RWMutex
	interval             time.Duration
	db                   RelayerDatabase
}

func NewDatabaseManager(logger logging.Logger, db RelayerDatabase, interval time.Duration) *DatabaseManager {
	return &DatabaseManager{
		logger:               logger,
		db:                   db,
		interval:             interval,
		committedHeights:     make(map[RelayerID]uint64),
		committedHeightsLock: &sync.RWMutex{},
	}
}

// Run writes all committed keys to the database on the configured timer.
// This function should only be called once.
func (dm *DatabaseManager) Run() {
	for range time.Tick(dm.interval) {
		dm.committedHeightsLock.RLock()
		for id, height := range dm.committedHeights {
			// Ensure we're not writing the default value
			if height == 0 {
				continue
			}
			storedHeight, err := getLatestProcessedBlockHeight(dm.db, id)
			if err != nil && !IsKeyNotFoundError(err) {
				dm.logger.Error(
					"Failed to get latest processed block height",
					zap.Error(err),
					zap.String("relayerID", id.ID.String()),
				)
				continue
			}
			if storedHeight >= height {
				continue
			}
			dm.logger.Debug(
				"Writing height",
				zap.Uint64("height", height),
				zap.String("relayerID", id.ID.String()),
			)
			err = dm.db.Put(id.ID, LatestProcessedBlockKey, []byte(strconv.FormatUint(height, 10)))
			if err != nil {
				dm.logger.Error(
					"Failed to write latest processed block height",
					zap.Error(err),
					zap.String("relayerID", id.ID.String()),
				)
				continue
			}
		}
		dm.committedHeightsLock.RUnlock()
	}
}

func (dm *DatabaseManager) RegisterRelayerID(id RelayerID) {
	dm.committedHeightsLock.Lock()
	defer dm.committedHeightsLock.Unlock()
	if _, ok := dm.committedHeights[id]; !ok {
		dm.committedHeights[id] = 0
	}
}

func (dm *DatabaseManager) CommitHeight(id RelayerID, height uint64) {
	dm.committedHeightsLock.Lock()
	defer dm.committedHeightsLock.Unlock()
	dm.committedHeights[id] = height
}

func (dm *DatabaseManager) GetCommittedHeight(id RelayerID) uint64 {
	dm.committedHeightsLock.RLock()
	defer dm.committedHeightsLock.RUnlock()
	return dm.committedHeights[id]
}

func (dm *DatabaseManager) Get(id RelayerID, key DataKey) ([]byte, error) {
	return dm.db.Get(id.ID, key)
}

// Helper function to get the latest processed block height from the database.
func getLatestProcessedBlockHeight(db RelayerDatabase, relayerID RelayerID) (uint64, error) {
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
