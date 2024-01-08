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
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/interfaces"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type CallContractChecker struct {
	input          []byte
	expectedResult []byte
	times          int
}

var (
	messageProtocolAddress = common.HexToHash("0xd81545385803bCD83bd59f58Ba2d2c0562387F83")
	messageProtocolConfig  = config.MessageProtocolConfig{
		MessageFormat: config.TELEPORTER.String(),
		Settings: map[string]interface{}{
			"reward-address": "0x27aE10273D17Cd7e80de8580A51f476960626e5f",
		},
	}
	destinationBlockchainIDString = "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD"
	messageIDstring               = "2CQw6XkzbDZY87XRomuszWkCBDTUvMaZv3YE2PAf7cicxWWEMF"
	validRelayerAddress           = common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567")
	validTeleporterMessage        = teleportermessenger.TeleporterMessage{
		MessageNonce:       big.NewInt(1),
		SenderAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		DestinationAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		RequiredGasLimit:   big.NewInt(2),
		AllowedRelayerAddresses: []common.Address{
			validRelayerAddress,
		},
		Receipts: []teleportermessenger.TeleporterMessageReceipt{
			{
				ReceivedMessageNonce: big.NewInt(1),
				RelayerRewardAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
		},
		Message: []byte{1, 2, 3, 4},
	}
)

func TestShouldSendMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	logger := logging.NoLog{}
	destinationBlockchainID, err := ids.FromString(destinationBlockchainIDString)
	require.NoError(t, err)

	mockClient := mock_vms.NewMockDestinationClient(ctrl)
	destinationClients := map[ids.ID]vms.DestinationClient{
		destinationBlockchainID: mockClient,
	}

	messageManager, err := NewMessageManager(
		logger,
		messageProtocolAddress,
		messageProtocolConfig,
		destinationClients,
	)
	require.NoError(t, err)

	validMessageBytes, err := teleportermessenger.PackTeleporterMessage(validTeleporterMessage)
	require.NoError(t, err)

	sourceBlockchainID := ids.Empty
	warpUnsignedMessage, err := warp.NewUnsignedMessage(0, sourceBlockchainID, validMessageBytes)
	require.NoError(t, err)

	// Create all the inputs and expected outputs for Teleporter calls
	calculateMessageIDInput, err := teleportermessenger.PackCalculateMessageID(
		sourceBlockchainID,
		destinationBlockchainID,
		validTeleporterMessage.MessageNonce,
	)
	require.NoError(t, err)

	messageID, err := ids.FromString(messageIDstring)
	require.NoError(t, err)

	messageIDOutput, err := teleportermessenger.PackCalculateMessageIDOutput(messageID)
	require.NoError(t, err)

	messageReceivedInput, err := teleportermessenger.PackMessageReceived(messageID)
	require.NoError(t, err)

	messageNotDelivered, err := teleportermessenger.PackMessageReceivedOutput(false)
	require.NoError(t, err)

	messageDelivered, err := teleportermessenger.PackMessageReceivedOutput(true)
	require.NoError(t, err)

	// Make a copy of the previous Warp message with a new Warp message ID
	validTeleporterMessage.MessageNonce = big.NewInt(2)
	validMessageBytes2, err := teleportermessenger.PackTeleporterMessage(validTeleporterMessage)
	require.NoError(t, err)
	warpUnsignedMessage2, err := warp.NewUnsignedMessage(0, sourceBlockchainID, validMessageBytes2)
	require.NoError(t, err)

	testCases := []struct {
		name                    string
		destinationBlockchainID ids.ID
		warpMessageInfo         *vmtypes.WarpMessageInfo
		senderAddressResult     common.Address
		senderAddressTimes      int
		clientTimes             int
		calculateMessageIDCall  *CallContractChecker
		messageReceivedCall     *CallContractChecker
		expectedError           bool
		expectedResult          bool
	}{
		{
			name:                    "valid message",
			destinationBlockchainID: destinationBlockchainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: validRelayerAddress,
			senderAddressTimes:  1,
			clientTimes:         1,
			calculateMessageIDCall: &CallContractChecker{
				input:          calculateMessageIDInput,
				expectedResult: messageIDOutput,
				times:          1,
			},
			messageReceivedCall: &CallContractChecker{
				input:          messageReceivedInput,
				expectedResult: messageNotDelivered,
				times:          1,
			},
			expectedResult: true,
		},
		{
			name:                    "invalid message",
			destinationBlockchainID: destinationBlockchainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage2,
				WarpPayload:         []byte{1, 2, 3, 4},
			},
			expectedError: true,
		},
		{
			name:                    "invalid destination chain id",
			destinationBlockchainID: ids.Empty,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			expectedError: true,
		},
		{
			name:                    "not allowed",
			destinationBlockchainID: destinationBlockchainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: common.Address{},
			senderAddressTimes:  1,
			clientTimes:         1,
			calculateMessageIDCall: &CallContractChecker{
				input:          calculateMessageIDInput,
				expectedResult: messageIDOutput,
				times:          1,
			},
			expectedResult: false,
		},
		{
			name:                    "message already delivered",
			destinationBlockchainID: destinationBlockchainID,
			warpMessageInfo: &vmtypes.WarpMessageInfo{
				WarpUnsignedMessage: warpUnsignedMessage,
				WarpPayload:         validMessageBytes,
			},
			senderAddressResult: validRelayerAddress,
			senderAddressTimes:  1,
			clientTimes:         1,
			calculateMessageIDCall: &CallContractChecker{
				input:          calculateMessageIDInput,
				expectedResult: messageIDOutput,
				times:          1,
			},
			messageReceivedCall: &CallContractChecker{
				input:          messageReceivedInput,
				expectedResult: messageDelivered,
				times:          1,
			},
			expectedResult: false,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			ethClient := mock_evm.NewMockClient(ctrl)
			mockClient.EXPECT().Client().Return(ethClient).Times(test.clientTimes)
			mockClient.EXPECT().SenderAddress().Return(test.senderAddressResult).Times(test.senderAddressTimes)
			protocolAddress := common.BytesToAddress(messageProtocolAddress[:])
			if test.calculateMessageIDCall != nil {
				messageIDInput := interfaces.CallMsg{From: bind.CallOpts{}.From, To: &protocolAddress, Data: test.calculateMessageIDCall.input}
				ethClient.EXPECT().CallContract(gomock.Any(), gomock.Eq(messageIDInput), gomock.Any()).Return(test.calculateMessageIDCall.expectedResult, nil).Times(test.calculateMessageIDCall.times)
			}
			if test.messageReceivedCall != nil {
				messageReceivedInput := interfaces.CallMsg{From: bind.CallOpts{}.From, To: &protocolAddress, Data: test.messageReceivedCall.input}
				ethClient.EXPECT().CallContract(gomock.Any(), gomock.Eq(messageReceivedInput), gomock.Any()).Return(test.messageReceivedCall.expectedResult, nil).Times(test.messageReceivedCall.times)
			}

			result, err := messageManager.ShouldSendMessage(test.warpMessageInfo, test.destinationBlockchainID)
			if test.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expectedResult, result)
			}
		})
	}
}
