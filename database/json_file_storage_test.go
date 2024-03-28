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
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/assert"
)

func createRelayerIDs(blockchainIDs []ids.ID) []RelayerID {
	destinationsBlockchainIDs := set.NewSet[string](1) // just needs to be non-nil
	destinationsBlockchainIDs.Add(ids.GenerateTestID().String())

	var relayerIDs []RelayerID
	for _, blockchainID := range blockchainIDs {
		for allowedDestination := range destinationsBlockchainIDs {
			id, _ := ids.FromString(allowedDestination)
			relayerIDs = append(relayerIDs, RelayerID{
				SourceBlockchainID:      blockchainID,
				DestinationBlockchainID: id,
			},
			)
		}
	}
	return relayerIDs
}

// Test that the JSON database can write and read to a single chain concurrently.
func TestConcurrentWriteReadSingleChain(t *testing.T) {
	relayerIDs := createRelayerIDs(
		[]ids.ID{
			ids.GenerateTestID(),
		},
	)
	jsonStorage := setupJsonStorage(t, relayerIDs)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, relayerIDs[0], uint64(idx))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(11)
	testWrite(jsonStorage, relayerIDs[0], finalTargetValue)

	latestProcessedBlockData, err := jsonStorage.Get(relayerIDs[0].GetID(), LatestProcessedBlockKey)
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
	relayerIDs := createRelayerIDs(
		[]ids.ID{
			ids.GenerateTestID(),
			ids.GenerateTestID(),
			ids.GenerateTestID(),
		},
	)
	jsonStorage := setupJsonStorage(t, relayerIDs)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		index := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, relayerIDs[index], uint64(index))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(3)
	for _, relayerID := range relayerIDs {
		testWrite(jsonStorage, relayerID, finalTargetValue)
	}

	for i, relayerID := range relayerIDs {
		latestProcessedBlockData, err := jsonStorage.Get(relayerID.GetID(), LatestProcessedBlockKey)
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

func setupJsonStorage(t *testing.T, relayerIDs []RelayerID) *JSONFileStorage {
	logger := logging.NewLogger(
		"awm-relayer-test",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)
	storageDir := t.TempDir()

	jsonStorage, err := NewJSONFileStorage(logger, storageDir, relayerIDs)
	if err != nil {
		t.Fatal(err)
	}
	return jsonStorage
}

func testWrite(storage *JSONFileStorage, relayerID RelayerID, height uint64) {
	err := storage.Put(relayerID.GetID(), LatestProcessedBlockKey, []byte(strconv.FormatUint(height, 10)))
	if err != nil {
		fmt.Printf("failed to put data: %v", err)
		return
	}
}
