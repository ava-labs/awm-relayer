package relayer

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	mock_database "github.com/ava-labs/awm-relayer/database/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func makeMessageRelayerWithMockDatabase(t *testing.T) (*messageRelayer, *mock_database.MockRelayerDatabase) {
	db := mock_database.NewMockRelayerDatabase(gomock.NewController(t))

	return &messageRelayer{
		logger: logging.NoLog{},
		db:     db,
	}, db
}

func TestCalculateStartingBlockHeight(t *testing.T) {
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
			dbError:       database.ErrKeyNotFound,
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
	}

	for _, testCase := range testCases {
		relayerUnderTest, db := makeMessageRelayerWithMockDatabase(t)
		db.
			EXPECT().
			Get(gomock.Any(), []byte(database.LatestProcessedBlockKey)).
			Return([]byte(strconv.FormatUint(testCase.dbBlock, 10)), testCase.dbError).
			Times(1)
		ret, err := relayerUnderTest.calculateStartingBlockHeight(testCase.cfgBlock)
		if testCase.expectedError == nil {
			require.NoError(t, err, fmt.Sprintf("test failed: %s", testCase.name))
			require.Equal(t, testCase.expectedBlock, ret, fmt.Sprintf("test failed: %s", testCase.name))
		} else {
			require.Error(t, err, fmt.Sprintf("test failed: %s", testCase.name))
		}
	}
}
