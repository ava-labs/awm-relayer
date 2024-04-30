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
// checkpointManager commits keys to be written to the database in a thread safe manner.
//

type checkpointManager struct {
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

func newCheckpointManager(
	logger logging.Logger,
	database database.RelayerDatabase,
	writeSignal chan struct{},
	relayerID database.RelayerID,
	startingHeight uint64,
) *checkpointManager {
	h := &utils.UInt64Heap{}
	heap.Init(h)
	return &checkpointManager{
		logger:                   logger,
		database:                 database,
		writeSignal:              writeSignal,
		relayerID:                relayerID,
		queuedHeightsAndMessages: make(map[uint64]*messageCounter),
		committedHeight:          startingHeight,
		lock:                     &sync.RWMutex{},
		pendingCommits:           h,
		finished:                 make(chan uint64),
	}
}

func (cm *checkpointManager) run() {
	go cm.listenForFinishedRelays()
	go cm.listenForWriteSignal()
}

func (cm *checkpointManager) writeToDatabase() {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	// Defensively ensure we're not writing the default value
	if cm.committedHeight == 0 {
		return
	}
	storedHeight, err := database.GetLatestProcessedBlockHeight(cm.database, cm.relayerID)
	if err != nil && !database.IsKeyNotFoundError(err) {
		cm.logger.Error(
			"Failed to get latest processed block height",
			zap.Error(err),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		return
	}
	if storedHeight >= cm.committedHeight {
		return
	}
	cm.logger.Debug(
		"Writing height",
		zap.Uint64("height", cm.committedHeight),
		zap.String("relayerID", cm.relayerID.ID.String()),
	)
	err = cm.database.Put(cm.relayerID.ID, database.LatestProcessedBlockKey, []byte(strconv.FormatUint(cm.committedHeight, 10)))
	if err != nil {
		cm.logger.Error(
			"Failed to write latest processed block height",
			zap.Error(err),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		return
	}
}

func (cm *checkpointManager) listenForWriteSignal() {
	for range cm.writeSignal {
		cm.writeToDatabase()
	}
}

func (cm *checkpointManager) incrementFinishedCounter(height uint64) {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	counter, ok := cm.queuedHeightsAndMessages[height]
	if !ok {
		// This is expected for Warp messages that are not associated with the height greater than the latest processed block height.
		// For example, on startup it is possible for an Application Relayer to re-process messages for a height that has already been committed.
		// This is also the case for manual Warp messages that are processed out-of-band.
		cm.logger.Debug(
			"Pending height not found",
			zap.Uint64("height", height),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		return
	}

	counter.processedMessages++
	cm.logger.Debug(
		"Received finished signal",
		zap.Uint64("height", height),
		zap.String("relayerID", cm.relayerID.ID.String()),
		zap.Uint64("processedMessages", counter.processedMessages),
		zap.Uint64("totalMessages", counter.totalMessages),
	)
	if counter.processedMessages == counter.totalMessages {
		cm.stageCommittedHeight(height)
		delete(cm.queuedHeightsAndMessages, height)
	}
}

// handleFinishedRelays listens for finished signals from the application relayer, and commits the
// height once all messages have been processed.
// This function should only be called once.
func (cm *checkpointManager) listenForFinishedRelays() {
	for height := range cm.finished {
		cm.incrementFinishedCounter(height)
	}
}

// stageCommittedHeight queues a height to be written to the database.
// Heights are committed in sequence, so if height is not exactly one
// greater than the current committedHeight, it is instead cached in memory
// to potentially be committed later.
// Requires that cm.lock be held
func (cm *checkpointManager) stageCommittedHeight(height uint64) {
	// First push the height onto the pending commits min heap
	// This will ensure that the heights are committed in order
	heap.Push(cm.pendingCommits, height)
	cm.logger.Debug(
		"Pending committed heights",
		zap.Any("pendingCommits", cm.pendingCommits),
		zap.Uint64("maxCommittedHeight", cm.committedHeight),
		zap.String("relayerID", cm.relayerID.ID.String()),
	)

	for cm.pendingCommits.Peek() == cm.committedHeight+1 {
		h := heap.Pop(cm.pendingCommits).(uint64)
		cm.logger.Debug(
			"Committing height",
			zap.Uint64("height", height),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		cm.committedHeight = h
		if cm.pendingCommits.Len() == 0 {
			break
		}
	}
}

// PrepareHeight sets the total number of messages to be processed at a given height.
// Once all messages have been processed, the height is eligible to be committed.
// It is up to the caller to determine if a height is eligible to be committed.
// This function is thread safe.
func (cm *checkpointManager) prepareHeight(height uint64, totalMessages uint64) {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	// Heights less than or equal to the committed height are not candidates to write to the database.
	// This is to ensure that writes are strictly increasing.
	if height <= cm.committedHeight {
		cm.logger.Debug(
			"Skipping height",
			zap.Uint64("height", height),
			zap.Uint64("committedHeight", cm.committedHeight),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		return
	}
	cm.logger.Debug(
		"Preparing height",
		zap.Uint64("height", height),
		zap.Uint64("totalMessages", totalMessages),
		zap.String("relayerID", cm.relayerID.ID.String()),
	)
	// Short circuit to staging the height if there are no messages to process
	if totalMessages == 0 {
		cm.stageCommittedHeight(height)
		return
	}
	cm.queuedHeightsAndMessages[height] = &messageCounter{
		totalMessages:     totalMessages,
		processedMessages: 0,
	}
}

// Helper type
type messageCounter struct {
	totalMessages     uint64
	processedMessages uint64
}
