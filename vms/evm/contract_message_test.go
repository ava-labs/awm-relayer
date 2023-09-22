// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// nolint: unused
// Used to create a valid unsigned message for testing. Should not be used directly in tests.
func createUnsignedMessage() *warp.UnsignedMessage {
	sourceChainID, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
	if err != nil {
		return nil
	}
	destinationChainID, err := ids.FromString("11111111111111111111111111111111LpoYY")
	if err != nil {
		return nil
	}

	payload, err := warpPayload.NewAddressedPayload(
		common.HexToAddress("27aE10273D17Cd7e80de8580A51f476960626e5f"),
		common.Hash(destinationChainID),
		common.HexToAddress("1234123412341234123412341234123412341234"),
		[]byte{},
	)
	if err != nil {
		return nil
	}

	unsignedMsg, err := warp.NewUnsignedMessage(0, sourceChainID, payload.Bytes())
	if err != nil {
		return nil
	}
	return unsignedMsg
}

func TestUnpack(t *testing.T) {
	mockLogger := logging.NewMockLogger(gomock.NewController(t))
	m := NewContractMessage(mockLogger, config.SourceSubnet{})

	testCases := []struct {
		name          string
		input         string
		networkID     uint32
		errorLogTimes int
		expectError   bool
	}{
		{
			name:          "valid",
			input:         "0000000000007fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d50000005200000000000027ae10273d17cd7e80de8580a51f476960626e5f0000000000000000000000000000000000000000000000000000000000000000123412341234123412341234123412341234123400000000",
			networkID:     0,
			errorLogTimes: 0,
			expectError:   false,
		},
		{
			name:          "invalid",
			errorLogTimes: 1,
			input:         "1000000000007fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d50000005200000000000027ae10273d17cd7e80de8580a51f476960626e5f0000000000000000000000000000000000000000000000000000000000000000123412341234123412341234123412341234123400000000",
			expectError:   true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input, err := hex.DecodeString(testCase.input)
			require.NoError(t, err)

			mockLogger.EXPECT().Error(gomock.Any(), gomock.Any()).Times(testCase.errorLogTimes)
			msg, err := m.UnpackWarpMessage(input)
			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.networkID, msg.WarpUnsignedMessage.NetworkID)
			}
		})
	}
}
