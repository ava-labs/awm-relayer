package database

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/common"
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
			dbError:       ErrKeyNotFound,
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
			dbError:       ErrKeyNotFound,
			expectedBlock: currentBlock,
			expectedError: nil,
		},
	}

	for _, testCase := range testCases {
		db := &mockDB{}
		db.getFunc = func(relayerID common.Hash, key DataKey) ([]byte, error) {
			return []byte(strconv.FormatUint(testCase.dbBlock, 10)), testCase.dbError
		}

		ret, err := CalculateStartingBlockHeight(logging.NoLog{}, db, RelayerID{}, testCase.cfgBlock, currentBlock)
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
	getFunc func(relayerID common.Hash, key DataKey) ([]byte, error)
}

func (m *mockDB) Get(relayerID common.Hash, key DataKey) ([]byte, error) {
	return m.getFunc(relayerID, key)
}

func (m *mockDB) Put(relayerID common.Hash, key DataKey, value []byte) error {
	return nil
}
