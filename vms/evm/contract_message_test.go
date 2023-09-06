// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestUnpack(t *testing.T) {
	ctrl := gomock.NewController(t)

	m := NewContractMessage(logging.NewMockLogger(ctrl), config.SourceSubnet{})

	testCases := []struct {
		input     string
		networkID uint32
	}{
		{
			input:     "0000000000007fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d50000005200000000000027ae10273d17cd7e80de8580a51f476960626e5f0000000000000000000000000000000000000000000000000000000000000000123412341234123412341234123412341234123400000000",
			networkID: 0,
		},
	}

	for _, testCase := range testCases {
		input, err := hex.DecodeString(testCase.input)
		if err != nil {
			t.Errorf("failed to decode test input: %v", err)
		}
		msg, err := m.UnpackWarpMessage(input)
		if err != nil {
			t.Errorf("failed to unpack message: %v", err)
		}

		assert.Equal(t, testCase.networkID, msg.WarpUnsignedMessage.NetworkID)
	}
}
