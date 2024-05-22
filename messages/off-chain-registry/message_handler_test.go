// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchainregistry

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	mock_evm "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	mock_vms "github.com/ava-labs/awm-relayer/vms/mocks"
	teleporterregistry "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/upgrades/TeleporterRegistry"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	messageProtocolAddress    = common.HexToAddress("0x0000000000000000000000000000000000000000")
	teleporterRegistryAddress = common.HexToAddress("0xd81545385803bCD83bd59f58Ba2d2c0562387F83")
	messageProtocolConfig     = config.MessageProtocolConfig{
		MessageFormat: config.OFF_CHAIN_REGISTRY.String(),
		Settings: map[string]interface{}{
			"teleporter-registry-address": teleporterRegistryAddress.Hex(),
		},
	}
	destinationBlockchainIDString = "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD"
	destinationBlockchainID       ids.ID
)

func init() {
	var err error
	destinationBlockchainID, err = ids.FromString(destinationBlockchainIDString)
	if err != nil {
		panic(err)
	}
}

type CallContractChecker struct {
	expectedAddress common.Address
	expectedError   error
	times           int
}

func TestShouldSendMessage(t *testing.T) {
	testCases := []struct {
		name                      string
		destinationBlockchainID   ids.ID
		entry                     teleporterregistry.ProtocolRegistryEntry
		isMessageInvalid          bool
		clientTimes               int
		getAddressFromVersionCall *CallContractChecker
		expectedError             bool
		expectedResult            bool
	}{
		{
			name:                    "address/version not registered",
			destinationBlockchainID: destinationBlockchainID,
			entry: teleporterregistry.ProtocolRegistryEntry{
				Version:         big.NewInt(1),
				ProtocolAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
			clientTimes: 1,
			getAddressFromVersionCall: &CallContractChecker{
				expectedError: errors.New(revertVersionNotFoundString),
				times:         1,
			},
			expectedError:  false,
			expectedResult: true,
		},
		{
			name:                    "address already registered under the same version",
			destinationBlockchainID: destinationBlockchainID,
			entry: teleporterregistry.ProtocolRegistryEntry{
				Version:         big.NewInt(1),
				ProtocolAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
			clientTimes: 1,
			getAddressFromVersionCall: &CallContractChecker{
				expectedAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"), // same as test.entry
				times:           1,
			},
			expectedError:  false,
			expectedResult: false,
		},
		{
			name:                    "address not registered, version already registered",
			destinationBlockchainID: destinationBlockchainID,
			entry: teleporterregistry.ProtocolRegistryEntry{
				Version:         big.NewInt(1),
				ProtocolAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
			clientTimes: 1,
			getAddressFromVersionCall: &CallContractChecker{
				expectedAddress: common.HexToAddress("0x1123456789abcdef0123456789abcdef01234567"), // different than test.entry
				times:           1,
			},
			expectedError:  false,
			expectedResult: false,
		},
		{
			name:                    "invalid message",
			destinationBlockchainID: destinationBlockchainID,
			entry: teleporterregistry.ProtocolRegistryEntry{
				Version:         big.NewInt(1),
				ProtocolAddress: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			},
			isMessageInvalid: true,
			clientTimes:      1,
			getAddressFromVersionCall: &CallContractChecker{
				expectedError: errors.New("unknown error"),
				times:         1,
			},
			expectedError:  true,
			expectedResult: false,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			logger := logging.NoLog{}

			mockClient := mock_vms.NewMockDestinationClient(ctrl)

			factory, err := NewMessageHandlerFactory(
				logger,
				messageProtocolConfig,
			)
			require.NoError(t, err)
			ethClient := mock_evm.NewMockClient(ctrl)
			mockClient.EXPECT().
				Client().
				Return(ethClient).
				Times(test.clientTimes)
			mockClient.EXPECT().DestinationBlockchainID().Return(test.destinationBlockchainID).AnyTimes()
			if test.getAddressFromVersionCall != nil {
				output, err := packGetAddressFromVersionOutput(test.getAddressFromVersionCall.expectedAddress)
				require.NoError(t, err)
				ethClient.EXPECT().
					CallContract(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(output, test.getAddressFromVersionCall.expectedError).
					Times(test.getAddressFromVersionCall.times)
			}

			// construct the signed message
			var unsignedMessage *warp.UnsignedMessage
			if test.isMessageInvalid {
				unsignedMessage = createInvalidRegistryUnsignedWarpMessage(t, test.entry, teleporterRegistryAddress, test.destinationBlockchainID)
			} else {
				unsignedMessage = createRegistryUnsignedWarpMessage(t, test.entry, teleporterRegistryAddress, test.destinationBlockchainID)
			}
			messageHandler, err := factory.NewMessageHandler(unsignedMessage)
			require.NoError(t, err)
			result, err := messageHandler.ShouldSendMessage(mockClient)
			if test.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expectedResult, result)
			}
		})
	}
}

func createRegistryUnsignedWarpMessage(
	t *testing.T,
	entry teleporterregistry.ProtocolRegistryEntry,
	teleporterRegistryAddress common.Address,
	blockchainID ids.ID,
) *warp.UnsignedMessage {
	payloadBytes, err := teleporterregistry.PackTeleporterRegistryWarpPayload(entry, teleporterRegistryAddress)
	require.NoError(t, err)

	addressedPayload, err := payload.NewAddressedCall(messageProtocolAddress[:], payloadBytes)
	require.NoError(t, err)

	unsignedMessage, err := warp.NewUnsignedMessage(
		constants.LocalID,
		blockchainID,
		addressedPayload.Bytes())
	require.NoError(t, err)

	return unsignedMessage
}

func createInvalidRegistryUnsignedWarpMessage(
	t *testing.T,
	entry teleporterregistry.ProtocolRegistryEntry,
	teleporterRegistryAddress common.Address,
	blockchainID ids.ID,
) *warp.UnsignedMessage {
	payloadBytes, err := teleporterregistry.PackTeleporterRegistryWarpPayload(entry, teleporterRegistryAddress)
	require.NoError(t, err)

	invalidAddressedPayload, err := payload.NewAddressedCall(messageProtocolAddress[:], append(payloadBytes, []byte{1, 2, 3, 4}...))
	require.NoError(t, err)

	invalidUnsignedMessage, err := warp.NewUnsignedMessage(
		constants.LocalID,
		blockchainID,
		invalidAddressedPayload.Bytes())
	require.NoError(t, err)

	return invalidUnsignedMessage
}

func packGetAddressFromVersionOutput(address common.Address) ([]byte, error) {
	abi, err := teleporterregistry.TeleporterRegistryMetaData.GetAbi()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get abi")
	}

	return abi.PackOutput("getAddressFromVersion", address)
}
