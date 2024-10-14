// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
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

func TestProposerHeightCatchup(t *testing.T) {
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
	mockClient.EXPECT().GetBlockByHeight(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, height uint64) ([]byte, error) {
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

	_, oldestHeight, ok := proposerHeights.timeToHeight.Oldest()
	require.True(t, ok)
	require.Equal(t, uint64(15), oldestHeight)
	optimalHeight := proposerHeights.GetOptimalHeight()
	require.Equal(t, uint64(14), optimalHeight)
}

func buildFakeBlockWithTime(t *testing.T, blockTime time.Time, height uint64) *block.BanffStandardBlock {
	// Create a fake block with a specific time and height
	// these don't need to refer to each other so generating random test IDs.
	b, err := block.NewBanffStandardBlock(blockTime, ids.GenerateTestID(), height, nil)
	require.NoError(t, err)
	return b
}
