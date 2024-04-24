// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"container/heap"
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
		keyManager := newKeyManager(logger, key)
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
			km.maxHeightLock.RLock()
			currMaxCommittedHeight := km.maxCommittedHeight
			km.maxHeightLock.RUnlock()
			if currMaxCommittedHeight == 0 {
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
			if storedHeight >= currMaxCommittedHeight {
				continue
			}
			dm.logger.Debug(
				"Writing height",
				zap.Uint64("height", currMaxCommittedHeight),
				zap.String("relayerID", id.ID.String()),
			)
			err = dm.db.Put(id.ID, LatestProcessedBlockKey, []byte(strconv.FormatUint(currMaxCommittedHeight, 10)))
			if err != nil {
				dm.logger.Error(
					"Failed to write latest processed block height",
					zap.Error(err),
					zap.String("relayerID", id.ID.String()),
				)
				continue
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
		dm.logger.Error("Key manager not found", zap.String("relayerID", id.ID.String()))
		return
	}
	km.finished <- height
}

// intHeap adapted from https://pkg.go.dev/container/heap#example-package-IntHeap
type intHeap []uint64

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h intHeap) Peek() uint64       { return h[0] }

func (h *intHeap) Push(x any) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	*h = append(*h, x.(uint64))
}

func (h *intHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

//
// keyManager commits keys to be written to the database in a thread safe manner.
//

type keyManager struct {
	logger                   logging.Logger
	relayerID                RelayerID
	queuedHeightsLock        *sync.RWMutex
	queuedHeightsAndMessages map[uint64]*messageCounter
	maxHeightLock            *sync.RWMutex
	maxCommittedHeight       uint64
	pendingCommits           *intHeap
	finished                 chan uint64
}

func newKeyManager(logger logging.Logger, relayerID RelayerID) *keyManager {
	h := &intHeap{}
	heap.Init(h)
	return &keyManager{
		logger:                   logger,
		relayerID:                relayerID,
		queuedHeightsLock:        &sync.RWMutex{},
		queuedHeightsAndMessages: make(map[uint64]*messageCounter),
		maxHeightLock:            &sync.RWMutex{},
		pendingCommits:           h,
		finished:                 make(chan uint64),
	}
}

// Run listens for finished signals from application relayers, and commits the
// height once all messages have been processed.
// This function should only be called once.
func (km *keyManager) run() {
	for height := range km.finished {
		km.queuedHeightsLock.RLock()
		counter, ok := km.queuedHeightsAndMessages[height]
		km.queuedHeightsLock.RUnlock()
		if !ok {
			km.logger.Error(
				"Pending height not found",
				zap.Uint64("height", height),
				zap.String("relayerID", km.relayerID.ID.String()),
			)
			continue
		}

		counter.processedMessages++
		km.logger.Debug(
			"Received finished signal",
			zap.Uint64("height", height),
			zap.String("relayerID", km.relayerID.ID.String()),
			zap.Uint64("processedMessages", counter.processedMessages),
			zap.Uint64("totalMessages", counter.totalMessages),
		)
		if counter.processedMessages == counter.totalMessages {
			km.commitHeight(height)
			km.queuedHeightsLock.Lock()
			delete(km.queuedHeightsAndMessages, height)
			km.queuedHeightsLock.Unlock()
		}
	}
}

// commitHeight marks a height as eligible to be written to the database.
func (km *keyManager) commitHeight(height uint64) {
	km.maxHeightLock.Lock()
	defer km.maxHeightLock.Unlock()
	if km.maxCommittedHeight == 0 {
		km.logger.Debug(
			"Committing initial height",
			zap.Uint64("height", height),
			zap.String("relayerID", km.relayerID.ID.String()),
		)
		km.maxCommittedHeight = height
		return
	}

	// First push the height onto the pending commits min heap
	// This will ensure that the heights are committed in order
	heap.Push(km.pendingCommits, height)
	km.logger.Debug(
		"Pending committed heights",
		zap.Any("pendingCommits", km.pendingCommits),
		zap.Uint64("maxCommittedHeight", km.maxCommittedHeight),
		zap.String("relayerID", km.relayerID.ID.String()),
	)

	for km.pendingCommits.Peek() == km.maxCommittedHeight+1 {
		h := heap.Pop(km.pendingCommits).(uint64)
		km.logger.Debug(
			"Committing height",
			zap.Uint64("height", height),
			zap.String("relayerID", km.relayerID.ID.String()),
		)
		km.maxCommittedHeight = h
		if km.pendingCommits.Len() == 0 {
			break
		}
	}
}

// PrepareHeight sets the total number of messages to be processed at a given height.
// Once all messages have been processed, the height is eligible to be committed.
// It is up to the caller to determine if a height is eligible to be committed.
// This function is thread safe.
func (km *keyManager) prepareHeight(height uint64, totalMessages uint64) {
	km.logger.Debug(
		"Preparing height",
		zap.Uint64("height", height),
		zap.Uint64("totalMessages", totalMessages),
		zap.String("relayerID", km.relayerID.ID.String()),
	)
	if totalMessages == 0 {
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
