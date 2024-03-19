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
	"github.com/ava-labs/awm-relayer/config"
	"github.com/stretchr/testify/assert"
)

var validSourceBlockchainConfig = &config.SourceBlockchain{
	RPCEndpoint:  "http://test.avax.network/ext/bc/C/rpc",
	WSEndpoint:   "ws://test.avax.network/ext/bc/C/ws",
	BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
	SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
	VM:           "evm",
	MessageContracts: map[string]config.MessageProtocolConfig{
		"0xd81545385803bCD83bd59f58Ba2d2c0562387F83": {
			MessageFormat: config.TELEPORTER.String(),
		},
	},
}

func createRelayerKeys(blockchainIDs []ids.ID) []RelayerKey {
	destinationsBlockchainIDs := set.NewSet[string](1) // just needs to be non-nil
	destinationsBlockchainIDs.Add(ids.GenerateTestID().String())

	var relayerKeys []RelayerKey
	for _, blockchainID := range blockchainIDs {
		for allowedDestination := range destinationsBlockchainIDs {
			id, _ := ids.FromString(allowedDestination)
			relayerKeys = append(relayerKeys, RelayerKey{
				SourceBlockchainID:      blockchainID,
				DestinationBlockchainID: id,
			},
			)
		}
	}
	return relayerKeys
}

// Test that the JSON database can write and read to a single chain concurrently.
func TestConcurrentWriteReadSingleChain(t *testing.T) {
	relayerKeys := createRelayerKeys(
		[]ids.ID{
			ids.GenerateTestID(),
		},
	)
	jsonStorage := setupJsonStorage(t, relayerKeys)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, relayerKeys[0], uint64(idx))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(11)
	testWrite(jsonStorage, relayerKeys[0], finalTargetValue)

	latestProcessedBlockData, err := jsonStorage.Get(relayerKeys[0].CalculateRelayerKey(), []byte(LatestProcessedBlockKey))
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
	relayerKeys := createRelayerKeys(
		[]ids.ID{
			ids.GenerateTestID(),
			ids.GenerateTestID(),
			ids.GenerateTestID(),
		},
	)
	jsonStorage := setupJsonStorage(t, relayerKeys)

	// Test writing to the JSON database concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		index := i
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, relayerKeys[index], uint64(index))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(3)
	for _, relayerKey := range relayerKeys {
		testWrite(jsonStorage, relayerKey, finalTargetValue)
	}

	for i, relayerKey := range relayerKeys {
		latestProcessedBlockData, err := jsonStorage.Get(relayerKey.CalculateRelayerKey(), []byte(LatestProcessedBlockKey))
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

func setupJsonStorage(t *testing.T, relayerKeys []RelayerKey) *JSONFileStorage {
	logger := logging.NewLogger(
		"awm-relayer-test",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)
	storageDir := t.TempDir()

	jsonStorage, err := NewJSONFileStorage(logger, storageDir, relayerKeys)
	if err != nil {
		t.Fatal(err)
	}
	return jsonStorage
}

func testWrite(storage *JSONFileStorage, relayerKey RelayerKey, height uint64) {
	err := storage.Put(relayerKey.CalculateRelayerKey(), []byte(LatestProcessedBlockKey), []byte(strconv.FormatUint(height, 10)))
	if err != nil {
		fmt.Printf("failed to put data: %v", err)
		return
	}
}
