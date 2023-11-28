// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

// Test that the JSON database can write and read to a single chain concurrently.
func TestConcurrentWriteReadSingleChain(t *testing.T) {
	networks := []ids.ID{
		ids.GenerateTestID(),
	}
	jsonStorage := setupJsonStorage(t, networks)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, networks[0], uint64(idx))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(11)
	testWrite(jsonStorage, networks[0], finalTargetValue)

	latestProcessedBlockData, err := jsonStorage.Get(networks[0], []byte(LatestProcessedBlockKey))
	if err != nil {
		t.Fatalf("failed to retrieve from JSON storage. err: %v", err)
	}
	latestProcessedBlock, success := new(big.Int).SetString(string(latestProcessedBlockData), 10)
	if !success {
		t.Fatalf("failed to convert latest block to big.Int. err: %v", err)
	}
	assert.Equal(t, finalTargetValue, latestProcessedBlock.Uint64(), "latest processed block height is not correct.")
}

// Test that the JSON database can write and read from multiple chains concurrently. Write to any given chain are not concurrent.
func TestConcurrentWriteReadMultipleChains(t *testing.T) {
	networks := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}
	jsonStorage := setupJsonStorage(t, networks)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		index := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, networks[index], uint64(index))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(3)
	for _, network := range networks {
		testWrite(jsonStorage, network, finalTargetValue)
	}

	for i, id := range networks {
		latestProcessedBlockData, err := jsonStorage.Get(id, []byte(LatestProcessedBlockKey))
		if err != nil {
			t.Fatalf("failed to retrieve from JSON storage. networkID: %d err: %v", i, err)
		}
		latestProcessedBlock, success := new(big.Int).SetString(string(latestProcessedBlockData), 10)
		if !success {
			t.Fatalf("failed to convert latest block to big.Int. err: %v", err)
		}
		assert.Equal(t, finalTargetValue, latestProcessedBlock.Uint64(), fmt.Sprintf("latest processed block height is not correct. networkID: %d", i))
	}
}

func setupJsonStorage(t *testing.T, networks []ids.ID) *JSONFileStorage {
	logger := logging.NewLogger(
		"awm-relayer-test",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)
	storageDir := t.TempDir()

	jsonStorage, err := NewJSONFileStorage(logger, storageDir, networks)
	if err != nil {
		t.Fatal(err)
	}
	return jsonStorage
}

func testWrite(storage *JSONFileStorage, blockchainID ids.ID, height uint64) {
	fmt.Println(blockchainID, height)
	err := storage.Put(blockchainID, []byte(LatestProcessedBlockKey), []byte(strconv.FormatUint(height, 10)))
	if err != nil {
		fmt.Printf("failed to put data: %v", err)
		return
	}
}
