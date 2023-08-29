// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
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

// Pack the SendCrossChainMessage event type. PackEvent is documented as not supporting struct types, so this should be used
// with caution. Here, we only use it for testing purposes. In a real setting, the Teleporter contract should pack the event.
func packSendCrossChainMessageEvent(destinationChainID common.Hash, message TeleporterMessage) ([]common.Hash, []byte, error) {
	return EVMTeleporterContractABI.PackEvent("SendCrossChainMessage", destinationChainID, message.MessageID, message)
}

func TestPackUnpackTeleporterMessage(t *testing.T) {
	var (
		messageID          int64 = 4
		destinationChainID       = common.HexToHash("0x03")
	)
	message := testTeleporterMessage(messageID)

	topics, b, err := packSendCrossChainMessageEvent(destinationChainID, message)
	require.NoError(t, err)

	// Three events where the first event topics[0] is the event signature.
	require.Equal(t, len(topics), 3)
	require.Equal(t, destinationChainID, topics[1])
	require.Equal(t, new(big.Int).SetInt64(messageID), topics[2].Big())

	unpacked, err := unpackTeleporterMessage(b)
	require.NoError(t, err)

	require.Equalf(t, message.MessageID, unpacked.MessageID, "message ids do not match. expected: %d actual: %d", message.MessageID.Uint64(), unpacked.MessageID.Uint64())
	require.Equalf(t, message.SenderAddress, unpacked.SenderAddress, "sender addresses do not match. expected: %s actual: %s", message.SenderAddress.Hex(), unpacked.SenderAddress.Hex())
	require.Equal(t, message.DestinationAddress, unpacked.DestinationAddress, "destination addresses do not match. expected: %s actual: %s", message.DestinationAddress.Hex(), unpacked.DestinationAddress.Hex())
	require.Equalf(t, message.RequiredGasLimit, unpacked.RequiredGasLimit, "required gas limits do not match. expected: %d actual: %d", message.RequiredGasLimit.Uint64(), unpacked.RequiredGasLimit.Uint64())

	for i := 0; i < len(message.AllowedRelayerAddresses); i++ {
		require.Equalf(t, unpacked.AllowedRelayerAddresses[i], message.AllowedRelayerAddresses[i], "allowed relayer addresses %d do not match.", i)
	}

	for i := 0; i < len(message.Receipts); i++ {
		require.Equal(t, message.Receipts[i].ReceivedMessageID, unpacked.Receipts[i].ReceivedMessageID)
		require.Equal(t, message.Receipts[i].RelayerRewardAddress, unpacked.Receipts[i].RelayerRewardAddress)
	}

	require.Truef(t, bytes.Equal(message.Message, unpacked.Message), "messages do not match. expected: %s actual: %s", hex.EncodeToString(message.Message), hex.EncodeToString(unpacked.Message))
}
