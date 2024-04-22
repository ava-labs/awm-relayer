// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"fmt"
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
	logger      logging.Logger
	keyManagers map[RelayerID]*keyManager
	interval    time.Duration
	db          RelayerDatabase
}

func NewDatabaseManager(logger logging.Logger, db RelayerDatabase, interval time.Duration, keys []RelayerID) *DatabaseManager {
	keyManagers := make(map[RelayerID]*keyManager)
	for _, key := range keys {
		keyManager := newKeyManager(logger)
		go keyManager.run()
		keyManagers[key] = keyManager
	}
	return &DatabaseManager{
		logger:      logger,
		db:          db,
		interval:    interval,
		keyManagers: keyManagers,
	}
}

// Run writes all committed keys to the database on the configured timer.
// This function should only be called once.
func (dm *DatabaseManager) Run() {
	for range time.Tick(dm.interval) {
		for id, km := range dm.keyManagers {
			// Ensure we're not writing the default value
			if km.maxCommittedHeight > 0 {
				storedHeight, err := getLatestProcessedBlockHeight(dm.db, id)
				if err != nil && !IsKeyNotFoundError(err) {
					dm.logger.Error("Failed to get latest processed block height", zap.Error(err))
					continue
				}
				if storedHeight >= km.maxCommittedHeight {
					continue
				}
				dm.logger.Debug(
					"writing height",
					zap.Uint64("height", km.maxCommittedHeight),
					zap.String("relayerID", id.ID.String()),
				)
				err = dm.db.Put(id.ID, LatestProcessedBlockKey, []byte(strconv.FormatUint(km.maxCommittedHeight, 10)))
				if err != nil {
					dm.logger.Error("Failed to write latest processed block height", zap.Error(err))
					continue
				}
			}
		}
	}
}

func (dm *DatabaseManager) Get(id RelayerID, key DataKey) ([]byte, error) {
	return dm.db.Get(id.ID, key)
}

func (dm *DatabaseManager) PrepareHeight(id RelayerID, height uint64, totalMessages uint64) error {
	km, ok := dm.keyManagers[id]
	if !ok {
		dm.logger.Error("Key manager not found", zap.String("relayerID", id.ID.String()))
		return fmt.Errorf("key manager not found")
	}
	km.prepareHeight(height, totalMessages)
	return nil
}

func (dm *DatabaseManager) Finished(id RelayerID, height uint64) {
	km, ok := dm.keyManagers[id]
	if !ok {
		// This is not an error, as this will occur if the database is not eligible for writing
		dm.logger.Debug("Key manager not found", zap.String("relayerID", id.ID.String()))
		return
	}
	km.finished <- height
}

//
// keyManager commits keys to be written to the database in a thread safe manner.
//

type keyManager struct {
	logger                   logging.Logger
	queuedHeightsLock        *sync.Mutex
	queuedHeightsAndMessages map[uint64]*messageCounter
	maxHeightLock            *sync.Mutex
	maxCommittedHeight       uint64
	finished                 chan uint64
}

func newKeyManager(logger logging.Logger) *keyManager {
	return &keyManager{
		logger:                   logger,
		queuedHeightsLock:        &sync.Mutex{},
		queuedHeightsAndMessages: make(map[uint64]*messageCounter),
		maxHeightLock:            &sync.Mutex{},
		finished:                 make(chan uint64),
	}
}

// Run listens for finished signals from application relayers, and commits the
// height once all messages have been processed.
// This function should only be called once.
func (km *keyManager) run() {
	for height := range km.finished {
		counter, ok := km.queuedHeightsAndMessages[height]
		if !ok {
			continue
		}
		counter.processedMessages++
		if counter.processedMessages == counter.totalMessages {
			km.commitHeight(height)
			delete(km.queuedHeightsAndMessages, height)
		}
	}
}

// commitHeight marks a height as eligible to be written to the database.
func (km *keyManager) commitHeight(height uint64) {
	if height > km.maxCommittedHeight {
		km.logger.Debug("committing height", zap.Uint64("height", height))
		km.maxCommittedHeight = height
	}
}

// PrepareHeight sets the total number of messages to be processed at a given height.
// Once all messages have been processed, the height is eligible to be committed.
// It is up to the caller to determine if a height is eligible to be committed.
// This function is thread safe.
func (km *keyManager) prepareHeight(height uint64, totalMessages uint64) {
	km.logger.Debug("preparing height", zap.Uint64("height", height))
	if totalMessages == 0 {
		km.maxHeightLock.Lock()
		defer km.maxHeightLock.Unlock()
		km.commitHeight(height)
		return
	}
	km.queuedHeightsLock.Lock()
	defer km.queuedHeightsLock.Unlock()
	km.queuedHeightsAndMessages[height] = &messageCounter{
		totalMessages:     totalMessages,
		processedMessages: 0,
	}
}

// Helper type
type messageCounter struct {
	totalMessages     uint64
	processedMessages uint64
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