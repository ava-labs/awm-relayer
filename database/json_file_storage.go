// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var _ RelayerDatabase = &JSONFileStorage{}

type chainState map[string]string

// JSONFileStorage implements RelayerDatabase
type JSONFileStorage struct {
	// the directory where the json files are stored
	dir string

	// Each network has its own mutex
	// The blockchainIDs used to index the JSONFileStorage are created at initialization
	// and are not modified afterwards, so we don't need to lock the map itself.
	mutexes      map[ids.ID]*sync.RWMutex
	logger       logging.Logger
	currentState map[ids.ID]chainState
}

// NewJSONFileStorage creates a new JSONFileStorage instance
func NewJSONFileStorage(logger logging.Logger, dir string, sourceBlockchains []*config.SourceBlockchain) (*JSONFileStorage, error) {
	storage := &JSONFileStorage{
		dir:          filepath.Clean(dir),
		mutexes:      make(map[ids.ID]*sync.RWMutex),
		logger:       logger,
		currentState: make(map[ids.ID]chainState),
	}

	for _, sourceBlockchain := range sourceBlockchains {
		sourceBlockchainID := sourceBlockchain.GetBlockchainID()
		storage.currentState[sourceBlockchainID] = make(chainState)
		storage.mutexes[sourceBlockchainID] = &sync.RWMutex{}
	}

	_, err := os.Stat(dir)
	if err == nil {
		// Directory already exists.
		// Read the existing storage.
		for _, sourceBlockchain := range sourceBlockchains {
			sourceBlockchainID := sourceBlockchain.GetBlockchainID()
			currentState, fileExists, err := storage.getCurrentState(sourceBlockchainID)
			if err != nil {
				return nil, err
			}
			if fileExists {
				storage.currentState[sourceBlockchainID] = currentState
			}
		}
		return storage, nil
	}

	// 0755: The owner can read, write, execute.
	// Everyone else can read and execute but not modify the file.
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		storage.logger.Error("failed to create directory",
			zap.String("dir", dir),
			zap.Error(err))
		return nil, err
	}

	return storage, nil
}

// Get the latest chain state from the JSON database, and retrieve the value from the key
func (s *JSONFileStorage) Get(blockchainID ids.ID, key []byte) ([]byte, error) {
	mutex, ok := s.mutexes[blockchainID]
	if !ok {
		return nil, errors.Wrap(
			ErrDatabaseMisconfiguration,
			fmt.Sprintf("database not configured for chain %s", blockchainID.String()),
		)
	}

	mutex.RLock()
	defer mutex.RUnlock()
	currentState, fileExists, err := s.getCurrentState(blockchainID)
	if err != nil {
		return nil, err
	}
	if !fileExists {
		return nil, ErrChainNotFound
	}

	var val string
	if val, ok = currentState[string(key)]; !ok {
		return nil, ErrKeyNotFound
	}

	return []byte(val), nil
}

// Helper to get the current state of a blockchainID. Not thread-safe.
func (s *JSONFileStorage) getCurrentState(blockchainID ids.ID) (chainState, bool, error) {
	currentState := make(chainState)
	fileExists, err := s.read(blockchainID, &currentState)
	if err != nil {
		s.logger.Error(
			"failed to read file",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, false, err
	}
	return currentState, fileExists, nil
}

// Put the value into the JSON database. Read the current chain state and overwrite the key, if it exists
// If the file corresponding to {blockchainID} does not exist, then it will be created
func (s *JSONFileStorage) Put(blockchainID ids.ID, key []byte, value []byte) error {
	mutex, ok := s.mutexes[blockchainID]
	if !ok {
		return errors.Wrap(
			ErrDatabaseMisconfiguration,
			fmt.Sprintf("database not configured for chain %s", blockchainID.String()),
		)
	}

	mutex.Lock()
	defer mutex.Unlock()

	// Update the in-memory state and write to disk
	s.currentState[blockchainID][string(key)] = string(value)
	return s.write(blockchainID, s.currentState[blockchainID])
}

// Write the value to the file. The caller is responsible for ensuring proper synchronization
func (s *JSONFileStorage) write(blockchainID ids.ID, v interface{}) error {
	fnlPath := filepath.Join(s.dir, blockchainID.String()+".json")
	tmpPath := fnlPath + ".tmp"

	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return err
	}

	// Write marshaled data to the temp file.
	// If  the write fails, the original file is not affected.
	// Set file permissiones to 0644 so only the owner can read and write.
	// Everyone else can only read. No one can execute the file.
	if err := os.WriteFile(tmpPath, b, 0644); err != nil {
		return errors.Wrap(err, "failed to write file")
	}

	// Move final file into place
	if err := os.Rename(tmpPath, fnlPath); err != nil {
		return errors.Wrap(err, "failed to rename file")
	}

	return nil
}

// Read from disk and unmarshal into v
// Returns a bool indicating whether the file exists, and an error.
// If an error is returned, the bool should be ignored.
// The caller is responsible for ensuring proper synchronization
func (s *JSONFileStorage) read(blockchainID ids.ID, v interface{}) (bool, error) {
	path := filepath.Join(s.dir, blockchainID.String()+".json")

	// If the file does not exist, return false, but do not return an error as this
	// is an expected case
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		s.logger.Debug(
			"file does not exist",
			zap.String("path", path),
			zap.Error(err),
		)
		return false, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return false, errors.Wrap(err, "failed to read file")
	}

	// Unmarshal data
	if err = json.Unmarshal(b, &v); err != nil {
		return false, errors.Wrap(err, "failed to unmarshal json file")
	}

	// Return true to indicate that the file exists and we read from it successfully
	return true, nil
}
