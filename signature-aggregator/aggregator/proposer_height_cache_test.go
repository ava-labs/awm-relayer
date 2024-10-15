// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/awm-relayer/peers/validators/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const startCurrentHeight = uint64(10)

func TestProposerHeightInit(t *testing.T) {
	logger := logging.NoLog{}
	updateInterval := time.Second * 2

	mockClient := mocks.NewMockCanonicalValidatorClient(gomock.NewController(t))
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight, nil)
	proposerHeights, err := NewProposerHeightCache(logger, mockClient, updateInterval)
	if err != nil {
		t.Errorf("Expected no error, but got %s", err)
	}

	currentHeight := atomic.LoadUint64(&proposerHeights.currentMaxHeight)
	require.Equal(t, startCurrentHeight, currentHeight)

	// When the cache is empty the optimal height should be the current height - 1
	require.Zero(t, proposerHeights.timeToHeight.Len())
	require.Equal(t, currentHeight-1, proposerHeights.GetOptimalHeight())
}

// Tests the number of requests to the client to catch up to the current height
// and confirms that the cache correctly expires the older than necessary blocks.
func TestProposerHeightUpdateData(t *testing.T) {
	logger := logging.NoLog{}
	updateInterval := time.Second * 2

	mockClient := mocks.NewMockCanonicalValidatorClient(gomock.NewController(t))
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight, nil)
	proposerHeights, err := NewProposerHeightCache(logger, mockClient, updateInterval)
	if err != nil {
		t.Errorf("Expected no error, but got %s", err)
	}

	currentHeight := atomic.LoadUint64(&proposerHeights.currentMaxHeight)
	require.Equal(t, startCurrentHeight, currentHeight)

	newHeight := uint64(20)
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(newHeight, nil).Times(1)
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, height uint64) ([]byte, error) {
			// TODO, make timings for the test less flaky
			// treat newHeight as time.Now
			// and block production as happening every 5 seconds.

			blockTime := time.Now().Add(-time.Second * time.Duration(5*(newHeight-height)))
			banffBlock := buildFakeBlockWithTime(t, blockTime, height)
			return banffBlock.Bytes(), nil
		}).Times(10)

	// Now we have entered 10 elements from 11 to 20
	// as verified with the mockClient but also should have evicted
	// the first 4 elements as they are older than 30 seconds
	proposerHeights.updateData()

	currentHeight = atomic.LoadUint64(&proposerHeights.currentMaxHeight)
	require.Equal(t, newHeight, currentHeight)
	require.Equal(t, 6, proposerHeights.timeToHeight.Len())

	// Confirm that the oldest height is 15
	// and that the resulting optimal height is 14 (15 - 1)
	_, oldestHeight, ok := proposerHeights.timeToHeight.Oldest()
	require.True(t, ok)
	require.Equal(t, uint64(15), oldestHeight)
	optimalHeight := proposerHeights.GetOptimalHeight()
	require.Equal(t, uint64(14), optimalHeight)
}

// Confirms that if we are very behind we only re-query upto [maxHeightdifference]
func TestProposerHeightCatchUp(t *testing.T) {
	logger := logging.NoLog{}
	updateInterval := time.Second * 2

	// Sets the current height to 10
	mockClient := mocks.NewMockCanonicalValidatorClient(gomock.NewController(t))
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight, nil)
	proposerHeights, err := NewProposerHeightCache(logger, mockClient, updateInterval)
	if err != nil {
		t.Errorf("Expected no error, but got %s", err)
	}
	// contents of the fake block are not important for this test
	fakeBlock := buildFakeBlockWithTime(t, time.Now(), startCurrentHeight)

	// Make the next call to GetCurrentHeight return 100
	// and confirm that the [getValidators] is only called up to [maxHeightdifference] times
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(uint64(100), nil).Times(1)
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).
		Return(fakeBlock.Bytes(), nil).Times(maxHeightDifference)

	proposerHeights.updateData()
}

// Confirms that if any API calls fail we don't update the cache or
// current max height
func TestProposerHeightWriteHeightErrors(t *testing.T) {
	logger := logging.NoLog{}
	updateInterval := time.Second * 2

	// Sets the current height to 10
	mockClient := mocks.NewMockCanonicalValidatorClient(gomock.NewController(t))
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight, nil)
	proposerHeights, err := NewProposerHeightCache(logger, mockClient, updateInterval)
	if err != nil {
		t.Errorf("Expected no error, but got %s", err)
	}
	// test failing to get a new height
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(uint64(0), errors.New("Failed	to get height")).Times(1)

	proposerHeights.updateData()
	require.Equal(t, startCurrentHeight, atomic.LoadUint64(&proposerHeights.currentMaxHeight))
	require.Zero(t, proposerHeights.timeToHeight.Len())

	// assert that GetCurrentHeight will be called 3 times for all of the following 3 failing tests
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight+1, nil).Times(3)
	// test failing to get a block
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).Return(nil, errors.New("Failed to get block")).Times(1)

	proposerHeights.updateData()
	require.Equal(t, startCurrentHeight, atomic.LoadUint64(&proposerHeights.currentMaxHeight))
	require.Zero(t, proposerHeights.timeToHeight.Len())

	// tests block that doesn't parse
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).Return([]byte{0xaa, 0xbb}, nil).Times(1)

	require.Equal(t, startCurrentHeight, atomic.LoadUint64(&proposerHeights.currentMaxHeight))
	require.Zero(t, proposerHeights.timeToHeight.Len())
	proposerHeights.updateData()

	// test apricot block with no timestamp encoded
	apricotBlock, _ := block.NewApricotStandardBlock(ids.GenerateTestID(), 23, nil)
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).Return(apricotBlock.Bytes(), nil).Times(1)

	proposerHeights.updateData()
	require.Equal(t, startCurrentHeight, atomic.LoadUint64(&proposerHeights.currentMaxHeight))
	require.Zero(t, proposerHeights.timeToHeight.Len())
}

// Test async ticker updates
func TestProposerHeightStart(t *testing.T) {
	logger := logging.NoLog{}
	updateInterval := time.Second * 2

	// Sets the current height to 10
	mockClient := mocks.NewMockCanonicalValidatorClient(gomock.NewController(t))
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(startCurrentHeight, nil)
	proposerHeights, err := NewProposerHeightCache(logger, mockClient, updateInterval)
	if err != nil {
		t.Errorf("Expected no error, but got %s", err)
	}
	// use new blocks so that we actually expire some blocks out of the cache
	newHeight := startCurrentHeight + 7
	mockClient.EXPECT().GetCurrentHeight(gomock.Any()).Return(newHeight, nil).Times(1)
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, height uint64) ([]byte, error) {
			// treat newHeight as time.Now
			// and block production as happening every 5 seconds.

			blockTime := time.Now().Add(-time.Second * time.Duration(5*(newHeight-height)))
			banffBlock := buildFakeBlockWithTime(t, blockTime, height)
			return banffBlock.Bytes(), nil
		}).Times(int(newHeight) - int(startCurrentHeight))

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	proposerHeights.Start(ctx)
	time.Sleep(updateInterval + time.Second)
	cancelFunc()

	currentHeight := atomic.LoadUint64(&proposerHeights.currentMaxHeight)
	require.Equal(t, newHeight, currentHeight)
	// Fetch the oldest height and confirm that it is 2 higher than the starting height
	// because 5*6 = 30 seconds and the cache should have expired the first 2 blocks by now
	proposerHeights.timeToHeightLock.RLock()
	_, height, ok := proposerHeights.timeToHeight.Oldest()
	proposerHeights.timeToHeightLock.RUnlock()
	require.True(t, ok)
	require.Equal(t, startCurrentHeight+2, height)
	// The optimal height should be the current height - 1 (startCurrentHeight + 1)
	require.Equal(t, height-1, proposerHeights.GetOptimalHeight())
}

func buildFakeBlockWithTime(t *testing.T, blockTime time.Time, height uint64) *block.BanffStandardBlock {
	// Create a fake block with a specific time and height
	// these don't need to refer to each other so generating random test IDs.
	b, err := block.NewBanffStandardBlock(blockTime, ids.GenerateTestID(), height, nil)
	require.NoError(t, err)
	return b
}
