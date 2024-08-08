package checkpoint

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/stretchr/testify/require"
)

func TestCalculateStartingBlockHeight(t *testing.T) {
	currentBlock := uint64(200) // Higher than any of the test case cfg or db values
	testCases := []struct {
		name          string
		cfgBlock      uint64
		dbBlock       uint64
		dbError       error
		expectedBlock uint64
		expectedError error
	}{
		{
			// Value in cfg, no value in db
			name:          "value in cfg, no value in db",
			cfgBlock:      100,
			dbBlock:       0,
			dbError:       ErrNotFound,
			expectedBlock: 100,
			expectedError: nil,
		},
		{
			// Unknown DB error
			name:          "unknown DB error",
			cfgBlock:      100,
			dbBlock:       0,
			dbError:       fmt.Errorf("unknown error"),
			expectedBlock: 0,
			expectedError: fmt.Errorf("unknown error"),
		},
		{
			// DB value greater than cfg value
			name:          "DB value greater than cfg value",
			cfgBlock:      100,
			dbBlock:       200,
			dbError:       nil,
			expectedBlock: 200,
			expectedError: nil,
		},
		{
			// cfg value greater than DB value
			name:          "cfg value greater than DB value",
			cfgBlock:      200,
			dbBlock:       100,
			dbError:       nil,
			expectedBlock: 200,
			expectedError: nil,
		},
		{
			// no DB value, no cfg value
			name:          "no DB value, no cfg value",
			cfgBlock:      0,
			dbBlock:       0,
			dbError:       ErrNotFound,
			expectedBlock: currentBlock,
			expectedError: nil,
		},
	}

	for _, testCase := range testCases {
		db := &mockDB{}
		db.getFunc = func(relayerID relayer.RelayerID) (uint64, error) {
			return testCase.dbBlock, testCase.dbError
		}

		ret, err := CalculateStartingBlockHeight(logging.NoLog{}, db, relayer.RelayerID{}, testCase.cfgBlock, currentBlock)
		if testCase.expectedError == nil {
			require.NoError(t, err, fmt.Sprintf("test failed: %s", testCase.name))
			require.Equal(t, testCase.expectedBlock, ret, fmt.Sprintf("test failed: %s", testCase.name))
		} else {
			require.Error(t, err, fmt.Sprintf("test failed: %s", testCase.name))
		}
	}
}

// in-package mock to allow for unit testing of non-receiver functions that use the RelayerDatabase interface
type mockDB struct {
	getFunc func(relayerID relayer.RelayerID) (uint64, error)
}

func (m *mockDB) GetLatestProcessedBlockHeight(relayerID relayer.RelayerID) (uint64, error) {
	return m.getFunc(relayerID)
}

func (m *mockDB) StoreLatestProcessedBlockHeight(relayerID relayer.RelayerID, value uint64) error {
	return nil
}
