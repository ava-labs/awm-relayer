// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func testTeleporterMessage(messageID int64) TeleporterMessage {
	m := TeleporterMessage{
		MessageID:          big.NewInt(messageID),
		SenderAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		DestinationAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		RequiredGasLimit:   big.NewInt(2),
		AllowedRelayerAddresses: []common.Address{
			common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		},
		Receipts: []TeleporterMessageReceipt{
			{
				ReceivedMessageID:    big.NewInt(1),
				RelayerRewardAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
		},
		Message: []byte{1, 2, 3, 4},
	}
	return m
}

func TestPackUnpackTeleporterMessage(t *testing.T) {
	message := testTeleporterMessage(4)

	b, err := PackTeleporterMessage(common.HexToHash("0x03"), message)
	if err != nil {
		t.Errorf("failed to pack teleporter message: %v", err)
		t.FailNow()
	}

	unpacked, err := unpackTeleporterMessage(b)
	if err != nil {
		t.Errorf("failed to unpack teleporter message: %v", err)
		t.FailNow()
	}

	if unpacked.MessageID.Cmp(message.MessageID) != 0 {
		t.Errorf("message ids do not match. expected: %d actual: %d", message.MessageID.Uint64(), unpacked.MessageID.Uint64())
	}
	if unpacked.SenderAddress != message.SenderAddress {
		t.Errorf("sender addresses do not match. expected: %s actual: %s", message.SenderAddress.Hex(), unpacked.SenderAddress.Hex())
	}
	if unpacked.DestinationAddress != message.DestinationAddress {
		t.Errorf("destination addresses do not match. expected: %s actual: %s", message.DestinationAddress.Hex(), unpacked.DestinationAddress.Hex())
	}
	if unpacked.RequiredGasLimit.Cmp(message.RequiredGasLimit) != 0 {
		t.Errorf("required gas limits do not match. expected: %d actual: %d", message.RequiredGasLimit.Uint64(), unpacked.RequiredGasLimit.Uint64())
	}
	for i := 0; i < len(message.AllowedRelayerAddresses); i++ {
		if unpacked.AllowedRelayerAddresses[i] != message.AllowedRelayerAddresses[i] {
			t.Errorf("allowed relayer addresses %d do not match. expected: %s actual: %s", i, message.AllowedRelayerAddresses[i].Hex(), unpacked.AllowedRelayerAddresses[i].Hex())
		}
	}
	for i := 0; i < len(message.Receipts); i++ {
		assert.Equal(t, 0, unpacked.Receipts[i].ReceivedMessageID.Cmp(message.Receipts[i].ReceivedMessageID))
		assert.Equal(t, message.Receipts[i].RelayerRewardAddress, unpacked.Receipts[i].RelayerRewardAddress)
	}

	if !bytes.Equal(unpacked.Message, message.Message) {
		t.Errorf("messages do not match. expected: %s actual: %s", hex.EncodeToString(message.Message), hex.EncodeToString(unpacked.Message))
	}
}
