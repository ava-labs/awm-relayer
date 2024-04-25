package relayer

import (
	"container/heap"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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
	dbManager := database.NewDatabaseManager(logging.NoLog{}, nil, 0)
	for _, test := range testCases {
		id := database.RelayerID{
			ID: common.BytesToHash(crypto.Keccak256([]byte(test.name))),
		}
		dbManager.RegisterRelayerID(id)
		dbManager.CommitHeight(id, test.currentMaxHeight)
		km := newKeyManager(logging.NoLog{}, dbManager, id)
		heap.Init(test.pendingHeights)
		km.pendingCommits = test.pendingHeights

		km.commitHeight(test.commitHeight)
		require.Equal(t, test.expectedMaxHeight, dbManager.GetCommittedHeight(id), test.name)
	}
}
