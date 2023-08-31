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
		go func() {
			defer wg.Done()
			testWrite(jsonStorage, networks[0], uint64(i))
		}()
	}
	wg.Wait()

	// Write one final time to ensure that concurrent writes don't cause any issues.
	finalTargetValue := uint64(11)
	testWrite(jsonStorage, networks[0], finalTargetValue)

	latestSeenBlockData, err := jsonStorage.Get(networks[0], []byte(LatestSeenBlockKey))
	if err != nil {
		t.Fatal(fmt.Sprintf("failed to retrieve from JSON storage. err: %v", err))
	}
	latestSeenBlock, success := new(big.Int).SetString(string(latestSeenBlockData), 10)
	if !success {
		t.Fatal(fmt.Sprintf("failed to convert latest block to big.Int. err: %v", err))
	}
	assert.Equal(t, finalTargetValue, latestSeenBlock.Uint64(), "latest seen block height is not correct.")

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
		latestSeenBlockData, err := jsonStorage.Get(id, []byte(LatestSeenBlockKey))
		if err != nil {
			t.Fatal(fmt.Sprintf("failed to retrieve from JSON storage. networkID: %d err: %v", i, err))
		}
		latestSeenBlock, success := new(big.Int).SetString(string(latestSeenBlockData), 10)
		if !success {
			t.Fatal(fmt.Sprintf("failed to convert latest block to big.Int. err: %v", err))
		}
		assert.Equal(t, finalTargetValue, latestSeenBlock.Uint64(), fmt.Sprintf("latest seen block height is not correct. networkID: %d", i))
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

func testWrite(storage *JSONFileStorage, chainID ids.ID, height uint64) {
	fmt.Println(chainID, height)
	err := storage.Put(chainID, []byte(LatestSeenBlockKey), []byte(strconv.FormatUint(height, 10)))
	if err != nil {
		fmt.Printf("failed to put data: %v", err)
		return
	}
}
