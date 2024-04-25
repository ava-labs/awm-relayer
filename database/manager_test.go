package database

import (
	"container/heap"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"
)

func TestCommitHeight(t *testing.T) {
	testCases := []struct {
		name              string
		currentMaxHeight  uint64
		commitHeight      uint64
		pendingHeights    *intHeap
		expectedMaxHeight uint64
	}{
		{
			name:              "commit height is the next height",
			currentMaxHeight:  10,
			commitHeight:      11,
			pendingHeights:    &intHeap{},
			expectedMaxHeight: 11,
		},
		{
			name:              "commit height is the next height with pending heights",
			currentMaxHeight:  10,
			commitHeight:      11,
			pendingHeights:    &intHeap{12, 13},
			expectedMaxHeight: 13,
		},
		{
			name:              "commit height is not the next height",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &intHeap{},
			expectedMaxHeight: 10,
		},
		{
			name:              "commit height is not the next height with pending heights",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &intHeap{13, 14},
			expectedMaxHeight: 10,
		},
		{
			name:              "commit height is not the next height with next height pending",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &intHeap{11},
			expectedMaxHeight: 12,
		},
	}

	for _, test := range testCases {
		km := newKeyManager(logging.NoLog{}, RelayerID{})
		heap.Init(test.pendingHeights)
		km.pendingCommits = test.pendingHeights
		km.maxCommittedHeight = test.currentMaxHeight

		km.commitHeight(test.commitHeight)
		require.Equal(t, test.expectedMaxHeight, km.maxCommittedHeight, test.name)
	}
}
