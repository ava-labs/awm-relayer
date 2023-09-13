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
	"github.com/stretchr/testify/assert"
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
