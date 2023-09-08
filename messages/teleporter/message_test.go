// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"bytes"
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
		messageID          int64       = 4
		destinationChainID common.Hash = common.HexToHash("0x03")
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

	require.Equal(t, message.MessageID, unpacked.MessageID)
	require.Equal(t, message.SenderAddress, unpacked.SenderAddress)
	require.Equal(t, message.DestinationAddress, unpacked.DestinationAddress)
	require.Equal(t, message.RequiredGasLimit, unpacked.RequiredGasLimit)

	for i := 0; i < len(message.AllowedRelayerAddresses); i++ {
		require.Equal(t, unpacked.AllowedRelayerAddresses[i], message.AllowedRelayerAddresses[i])
	}

	for i := 0; i < len(message.Receipts); i++ {
		require.Equal(t, message.Receipts[i].ReceivedMessageID, unpacked.Receipts[i].ReceivedMessageID)
		require.Equal(t, message.Receipts[i].RelayerRewardAddress, unpacked.Receipts[i].RelayerRewardAddress)
	}

	require.True(t, bytes.Equal(message.Message, unpacked.Message))
}
