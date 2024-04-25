// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"container/heap"
	"strconv"
	"sync"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/utils"
	"go.uber.org/zap"
)

//
// keyManager commits keys to be written to the database in a thread safe manner.
//

type keyManager struct {
	logger                   logging.Logger
	database                 database.RelayerDatabase
	writeSignal              chan struct{}
	relayerID                database.RelayerID
	queuedHeightsAndMessages map[uint64]*messageCounter
	committedHeight          uint64
	lock                     *sync.RWMutex
	pendingCommits           *utils.UInt64Heap
	finished                 chan uint64
}

func newKeyManager(logger logging.Logger, database database.RelayerDatabase, writeSignal chan struct{}, relayerID database.RelayerID) *keyManager {
	h := &utils.UInt64Heap{}
	heap.Init(h)
	return &keyManager{
		logger:                   logger,
		database:                 database,
		writeSignal:              writeSignal,
		relayerID:                relayerID,
		queuedHeightsAndMessages: make(map[uint64]*messageCounter),
		lock:                     &sync.RWMutex{},
		pendingCommits:           h,
		finished:                 make(chan uint64),
	}
}

func (km *keyManager) run() {
	go km.handleFinishedRelays()
	go km.writeToDatabase()
}

func (km *keyManager) writeToDatabase() {
	for range km.writeSignal {
		km.lock.RLock()
		// Ensure we're not writing the default value
		if km.committedHeight == 0 {
			km.lock.RUnlock()
			continue
		}
		storedHeight, err := getLatestProcessedBlockHeight(km.database, km.relayerID)
		if err != nil && !database.IsKeyNotFoundError(err) {
			km.logger.Error(
				"Failed to get latest processed block height",
				zap.Error(err),
				zap.String("relayerID", km.relayerID.ID.String()),
			)
			continue
		}
		if storedHeight >= km.committedHeight {
			km.lock.RUnlock()
			continue
		}
		km.logger.Debug(
			"Writing height",
			zap.Uint64("height", km.committedHeight),
			zap.String("relayerID", km.relayerID.ID.String()),
		)
		err = km.database.Put(km.relayerID.ID, database.LatestProcessedBlockKey, []byte(strconv.FormatUint(km.committedHeight, 10)))
		if err != nil {
			km.logger.Error(
				"Failed to write latest processed block height",
				zap.Error(err),
				zap.String("relayerID", km.relayerID.ID.String()),
			)
			km.lock.RUnlock()
			continue
		}
		km.lock.RUnlock()
	}
}

// handleFinishedRelays listens for finished signals from the application relayer, and commits the
// height once all messages have been processed.
// This function should only be called once.
func (km *keyManager) handleFinishedRelays() {
	for height := range km.finished {
		km.lock.Lock()
		counter, ok := km.queuedHeightsAndMessages[height]
		if !ok {
			km.logger.Error(
				"Pending height not found",
				zap.Uint64("height", height),
				zap.String("relayerID", km.relayerID.ID.String()),
			)
			km.lock.Unlock()
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
			delete(km.queuedHeightsAndMessages, height)
		}
		km.lock.Unlock()
	}
}

// commitHeight marks a height as eligible to be written to the database.
// Requires that km.lock be held
func (km *keyManager) commitHeight(height uint64) {
	if km.committedHeight == 0 {
		km.logger.Debug(
			"Committing initial height",
			zap.Uint64("height", height),
			zap.String("relayerID", km.relayerID.ID.String()),
		)
		km.committedHeight = height
		return
	}

	// First push the height onto the pending commits min heap
	// This will ensure that the heights are committed in order
	heap.Push(km.pendingCommits, height)
	km.logger.Debug(
		"Pending committed heights",
		zap.Any("pendingCommits", km.pendingCommits),
		zap.Uint64("maxCommittedHeight", km.committedHeight),
		zap.String("relayerID", km.relayerID.ID.String()),
	)

	for km.pendingCommits.Peek() == km.committedHeight+1 {
		h := heap.Pop(km.pendingCommits).(uint64)
		km.logger.Debug(
			"Committing height",
			zap.Uint64("height", height),
			zap.String("relayerID", km.relayerID.ID.String()),
		)
		km.committedHeight = h
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
	km.lock.Lock()
	defer km.lock.Unlock()
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
func getLatestProcessedBlockHeight(db database.RelayerDatabase, relayerID database.RelayerID) (uint64, error) {
	latestProcessedBlockData, err := db.Get(relayerID.ID, database.LatestProcessedBlockKey)
	if err != nil {
		return 0, err
	}
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		return 0, err
	}
	return latestProcessedBlock, nil
}
