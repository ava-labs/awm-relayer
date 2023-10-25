// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	mock_evm "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	mock_vms "github.com/ava-labs/awm-relayer/vms/mocks"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	messageProtocolAddress = common.HexToHash("0xd81545385803bCD83bd59f58Ba2d2c0562387F83")
	messageProtocolConfig  = config.MessageProtocolConfig{
		MessageFormat: config.TELEPORTER.String(),
		Settings: map[string]interface{}{
			"reward-address": "0x27aE10273D17Cd7e80de8580A51f476960626e5f",
		},
	}
	destinationChainIDString = "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD"
	validRelayerAddress      = common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567")
	validTeleporterMessage   = teleportermessenger.TeleporterMessage{
		MessageID:          big.NewInt(1),
		SenderAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		DestinationAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		RequiredGasLimit:   big.NewInt(2),
		AllowedRelayerAddresses: []common.Address{
			validRelayerAddress,
		},
		Receipts: []teleportermessenger.TeleporterMessageReceipt{
			{
				ReceivedMessageID:    big.NewInt(1),
				RelayerRewardAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
		},
		Message: []byte{1, 2, 3, 4},
	}
)

func TestShouldSendMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	logger := logging.NoLog{}
	destinationChainID, err := ids.FromString(destinationChainIDString)
	require.NoError(t, err)

	mockClient := mock_vms.NewMockDestinationClient(ctrl)
	destinationClients := map[ids.ID]vms.DestinationClient{
		destinationChainID: mockClient,
	}

	messageManager, err := NewMessageManager(
		logger,
		messageProtocolAddress,
		messageProtocolConfig,
		destinationClients,
	)
	require.NoError(t, err)

	validMessageBytes, err := teleportermessenger.PackSendCrossChainMessageEvent(common.HexToHash(destinationChainID.Hex()), validTeleporterMessage)
	require.NoError(t, err)

	messageNotDelivered, err := teleportermessenger.PackMessageReceivedOutput(false)
	require.NoError(t, err)

	messageDelivered, err := teleportermessenger.PackMessageReceivedOutput(true)
	require.NoError(t, err)

	warpUnsignedMessage, err := warp.NewUnsignedMessage(0, ids.Empty, validMessageBytes)
	require.NoError(t, err)
	testCases := []struct {
		name                string
		destinationChainID  ids.ID
		warpMessageInfo     *vmtypes.WarpMessageInfo
		senderAddressResult common.Address
		senderAddressTimes  int
		clientResult        *mock_evm.MockClient
		clientTimes         int
		callContractResult  []byte
		callContractTimes   int
		expectedError       bool
		expectedResult      bool
	}{
		{
			name:               "valid message",
			destinationChainID: destinationChainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: validRelayerAddress,
			senderAddressTimes:  1,
			clientResult:        mock_evm.NewMockClient(ctrl),
			clientTimes:         1,
			callContractResult:  messageNotDelivered,
			callContractTimes:   1,
			expectedResult:      true,
		},
		{
			name:               "invalid message",
			destinationChainID: destinationChainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         []byte{1, 2, 3, 4},
			},
			expectedError: true,
		},
		{
			name:               "invalid destination chain id",
			destinationChainID: ids.Empty,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			expectedError: true,
		},
		{
			name:               "not allowed",
			destinationChainID: destinationChainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: common.Address{},
			senderAddressTimes:  1,
			expectedResult:      false,
		},
		{
			name:               "message already delivered",
			destinationChainID: destinationChainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: validRelayerAddress,
			senderAddressTimes:  1,
			clientResult:        mock_evm.NewMockClient(ctrl),
			clientTimes:         1,
			callContractResult:  messageDelivered,
			callContractTimes:   1,
			expectedResult:      false,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mockClient.EXPECT().SenderAddress().Return(test.senderAddressResult).Times(test.senderAddressTimes)
			mockClient.EXPECT().Client().Return(test.clientResult).Times(test.clientTimes)
			if test.clientResult != nil {
				test.clientResult.EXPECT().CallContract(gomock.Any(), gomock.Any(), gomock.Any()).Return(test.callContractResult, nil).Times(test.callContractTimes)
			}

			result, err := messageManager.ShouldSendMessage(test.warpMessageInfo, test.destinationChainID)
			if test.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expectedResult, result)
			}
		})
	}
}
