// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package checkpoint

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
// CheckpointManager commits keys to be written to the database in a thread safe manner.
//

type CheckpointManager struct {
	logger          logging.Logger
	database        database.RelayerDatabase
	writeSignal     chan struct{}
	relayerID       database.RelayerID
	committedHeight uint64
	lock            *sync.RWMutex
	pendingCommits  *utils.UInt64Heap
}

func NewCheckpointManager(
	logger logging.Logger,
	database database.RelayerDatabase,
	writeSignal chan struct{},
	relayerID database.RelayerID,
	startingHeight uint64,
) *CheckpointManager {
	h := &utils.UInt64Heap{}
	heap.Init(h)
	return &CheckpointManager{
		logger:          logger,
		database:        database,
		writeSignal:     writeSignal,
		relayerID:       relayerID,
		committedHeight: startingHeight,
		lock:            &sync.RWMutex{},
		pendingCommits:  h,
	}
}

func (cm *CheckpointManager) Run() {
	go cm.listenForWriteSignal()
}

func (cm *CheckpointManager) writeToDatabase() {
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

func (cm *CheckpointManager) listenForWriteSignal() {
	for range cm.writeSignal {
		cm.writeToDatabase()
	}
}

// StageCommittedHeight queues a height to be written to the database.
// Heights are committed in sequence, so if height is not exactly one
// greater than the current committedHeight, it is instead cached in memory
// to potentially be committed later.
// Requires that cm.lock be held
func (cm *CheckpointManager) StageCommittedHeight(height uint64) {
	if height <= cm.committedHeight {
		cm.logger.Fatal(
			"Attempting to commit height less than or equal to the committed height",
			zap.Uint64("height", height),
			zap.Uint64("committedHeight", cm.committedHeight),
			zap.String("relayerID", cm.relayerID.ID.String()),
		)
		panic("attempting to commit height less than or equal to the committed height")
	}

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
