// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"strconv"
	"time"
)

//
// KeyManager commits keys to be written to the database in a thread safe manner
//

type KeyManager struct {
	queuedHeightsAndMessages map[uint64]*messageCounter
	id                       RelayerID
	maxCommittedHeight       uint64
	finished                 chan uint64
}

func NewKeyManager(id RelayerID) *KeyManager {
	return &KeyManager{
		queuedHeightsAndMessages: make(map[uint64]*messageCounter),
		id:                       id,
		finished:                 make(chan uint64),
	}
}

// Run listens for finished signals from application relayers, and commits the
// height once all messages have been processed
func (km *KeyManager) Run() {
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

// commitHeight marks a height as eligible to be written to the database
func (km *KeyManager) commitHeight(height uint64) {
	if height > km.maxCommittedHeight {
		km.maxCommittedHeight = height
	}
}

func (km *KeyManager) ID() RelayerID {
	return km.id
}

// PrepareHeight sets the total number of messages to be processed at a given height
// Once all messages have been processed, the height is eligible to be committed
func (km *KeyManager) PrepareHeight(height uint64, totalMessages uint64) {
	if totalMessages == 0 {
		km.commitHeight(height)
		return
	}
	km.queuedHeightsAndMessages[height] = &messageCounter{
		totalMessages:     totalMessages,
		processedMessages: 0,
	}
}

//
// DatabaseManager writes all committed keys to the database on a timer
//

type DatabaseManager struct {
	keyManagers []*KeyManager
	interval    time.Duration
	db          RelayerDatabase
}

func NewDataBaseManager(db RelayerDatabase, interval time.Duration, keys []RelayerID) *DatabaseManager {
	keyManagers := make([]*KeyManager, len(keys))
	for i, key := range keys {
		keyManagers[i] = NewKeyManager(key)
	}
	return &DatabaseManager{
		db:          db,
		interval:    interval,
		keyManagers: keyManagers,
	}
}

// Run writes all committed keys to the database on the configured timer
func (dm *DatabaseManager) Run() {
	for range time.Tick(dm.interval) {
		for _, km := range dm.keyManagers {
			// Ensure we're not writing the default value
			if km.maxCommittedHeight > 0 {
				dm.db.Put(km.ID().ID, LatestProcessedBlockKey, []byte(strconv.FormatUint(km.maxCommittedHeight, 10)))
			}
		}
	}
}

// Helper type
type messageCounter struct {
	totalMessages     uint64
	processedMessages uint64
}
