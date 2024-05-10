package checkpoint

import (
	"container/heap"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	mock_database "github.com/ava-labs/awm-relayer/database/mocks"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCommitHeight(t *testing.T) {
	testCases := []struct {
		name              string
		currentMaxHeight  uint64
		commitHeight      uint64
		pendingHeights    *utils.UInt64Heap
		expectedMaxHeight uint64
	}{
		{
			name:              "commit height is the next height",
			currentMaxHeight:  10,
			commitHeight:      11,
			pendingHeights:    &utils.UInt64Heap{},
			expectedMaxHeight: 11,
		},
		{
			name:              "commit height is the next height with pending heights",
			currentMaxHeight:  10,
			commitHeight:      11,
			pendingHeights:    &utils.UInt64Heap{12, 13},
			expectedMaxHeight: 13,
		},
		{
			name:              "commit height is not the next height",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &utils.UInt64Heap{},
			expectedMaxHeight: 10,
		},
		{
			name:              "commit height is not the next height with pending heights",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &utils.UInt64Heap{13, 14},
			expectedMaxHeight: 10,
		},
		{
			name:              "commit height is not the next height with next height pending",
			currentMaxHeight:  10,
			commitHeight:      12,
			pendingHeights:    &utils.UInt64Heap{11},
			expectedMaxHeight: 12,
		},
	}
	db := mock_database.NewMockRelayerDatabase(gomock.NewController(t))
	for _, test := range testCases {
		id := database.RelayerID{
			ID: common.BytesToHash(crypto.Keccak256([]byte(test.name))),
		}
		cm := NewCheckpointManager(logging.NoLog{}, db, nil, id, test.currentMaxHeight)
		heap.Init(test.pendingHeights)
		cm.pendingCommits = test.pendingHeights
		cm.committedHeight = test.currentMaxHeight
		cm.StageCommittedHeight(test.commitHeight)
		require.Equal(t, test.expectedMaxHeight, cm.committedHeight, test.name)
	}
}
