package block_hash_publisher

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/stretchr/testify/require"
)

func TestShouldSendMessage(t *testing.T) {
	testCases := []struct {
		name            string
		chainID         ids.ID
		destination     destinationSenderInfo
		warpMessageInfo vmtypes.WarpMessageInfo
		expectedError   bool
		expectedResult  bool
	}{
		{
			name:    "should send (time)",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval:     true,
				timeIntervalSeconds: 10,
				lastTimeSent:        uint64(time.Unix(100, 0).Unix()),
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockTimestamp: uint64(time.Unix(111, 0).Unix()),
			},
			expectedError:  false,
			expectedResult: true,
		},
		{
			name:    "should send (block)",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval: false,
				blockInterval:   5,
				lastBlock:       100,
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockNumber: 106,
			},
			expectedError:  false,
			expectedResult: true,
		},
		{
			name:    "should send (time) 2",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval:     true,
				timeIntervalSeconds: 10,
				lastTimeSent:        uint64(time.Unix(100, 0).Unix()),
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockTimestamp: uint64(time.Unix(110, 0).Unix()),
			},
			expectedError:  false,
			expectedResult: true,
		},
		{
			name:    "should send (block) 2",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval: false,
				blockInterval:   5,
				lastBlock:       100,
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockNumber: 105,
			},
			expectedError:  false,
			expectedResult: true,
		},
		{
			name:    "should not send (time)",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval:     true,
				timeIntervalSeconds: 10,
				lastTimeSent:        uint64(time.Unix(100, 0).Unix()),
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockTimestamp: uint64(time.Unix(109, 0).Unix()),
			},
			expectedError:  false,
			expectedResult: false,
		},
		{
			name:    "should not send (block)",
			chainID: ids.GenerateTestID(),
			destination: destinationSenderInfo{
				useTimeInterval: false,
				blockInterval:   5,
				lastBlock:       100,
			},
			warpMessageInfo: vmtypes.WarpMessageInfo{
				BlockNumber: 104,
			},
			expectedError:  false,
			expectedResult: false,
		},
	}
	for _, testCase := range testCases {
		messageManager := &messageManager{
			destinations: map[ids.ID]*destinationSenderInfo{
				testCase.chainID: &testCase.destination,
			},
		}
		result, err := messageManager.ShouldSendMessage(&testCase.warpMessageInfo, testCase.chainID)
		if testCase.expectedError {
			require.Error(t, err, testCase.name)
		} else {
			require.NoError(t, err, testCase.name)
			require.Equal(t, testCase.expectedResult, result, testCase.name)
		}
	}
}
