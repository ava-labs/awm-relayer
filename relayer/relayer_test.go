// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	mock_database "github.com/ava-labs/awm-relayer/database/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var id1 ids.ID = ids.GenerateTestID()
var id2 ids.ID = ids.GenerateTestID()

func makeRelayerWithMockDatabase(t *testing.T) (*Relayer, *mock_database.MockRelayerDatabase) {
	mockDatabase := mock_database.NewMockRelayerDatabase(gomock.NewController(t))

	blockchainID, err := ids.FromString("S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD")
	require.NoError(t, err)

	logger := logging.NewLogger(
		"awm-relayer-test",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	return &Relayer{
		sourceBlockchainID: blockchainID,
		db:                 mockDatabase,
		logger:             logger,
	}, mockDatabase
}

func TestCheckSupportedDestination(t *testing.T) {
	testCases := []struct {
		name                    string
		relayer                 Relayer
		destinationBlockchainID ids.ID
		expectedResult          bool
	}{
		{
			name: "explicitly supported destination",
			relayer: Relayer{
				supportedDestinations: set.Set[ids.ID]{
					id1: {},
				},
				globalConfig: config.Config{
					DestinationSubnets: []*config.DestinationSubnet{
						{
							BlockchainID: id1.String(),
						},
					},
				},
			},
			destinationBlockchainID: id1,
			expectedResult:          true,
		},
		{
			name: "implicitly supported destination",
			relayer: Relayer{
				globalConfig: config.Config{
					DestinationSubnets: []*config.DestinationSubnet{
						{
							BlockchainID: id1.String(),
						},
					},
				},
			},
			destinationBlockchainID: id1,
			expectedResult:          true,
		},
		{
			name: "unsupported destination",
			relayer: Relayer{
				supportedDestinations: set.Set[ids.ID]{
					id1: {},
				},
				globalConfig: config.Config{
					DestinationSubnets: []*config.DestinationSubnet{
						{
							BlockchainID: id1.String(),
						},
					},
				},
			},
			destinationBlockchainID: id2,
			expectedResult:          false,
		},
	}

	for _, testCase := range testCases {
		result := testCase.relayer.CheckSupportedDestination(testCase.destinationBlockchainID)
		require.Equal(t, testCase.expectedResult, result, fmt.Sprintf("test failed: %s", testCase.name))
	}
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
			// No value in cfg or db
			name:          "no value in cfg or db",
			cfgBlock:      0,
			dbBlock:       0,
			dbError:       database.ErrKeyNotFound,
			expectedBlock: 0,
			expectedError: ErrNoStartBlock,
		},
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
		relayerUnderTest, db := makeRelayerWithMockDatabase(t)
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
