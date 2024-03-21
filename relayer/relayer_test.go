// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var id1 ids.ID = ids.GenerateTestID()
var id2 ids.ID = ids.GenerateTestID()

var validSourceBlockchainConfig = &config.SourceBlockchain{
	RPCEndpoint:  "http://test.avax.network/ext/bc/C/rpc",
	WSEndpoint:   "ws://test.avax.network/ext/bc/C/ws",
	BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
	SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
	VM:           "evm",
	MessageContracts: map[string]config.MessageProtocolConfig{
		"0xd81545385803bCD83bd59f58Ba2d2c0562387F83": {
			MessageFormat: config.TELEPORTER.String(),
		},
	},
}

var validDestinationBlockchainConfig = &config.DestinationBlockchain{
	SubnetID:          "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
	BlockchainID:      "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
	VM:                "evm",
	RPCEndpoint:       "http://test.avax.network/ext/bc/C/rpc",
	AccountPrivateKey: "1234567890123456789012345678901234567890123456789012345678901234",
}

func populateSourceConfig(blockchainIDs []ids.ID, supportedDestinations []string, allowedAddresses []string) []*config.SourceBlockchain {
	sourceBlockchains := make([]*config.SourceBlockchain, len(blockchainIDs))
	for i, id := range blockchainIDs {
		sourceBlockchains[i] = validSourceBlockchainConfig
		sourceBlockchains[i].BlockchainID = id.String()
		sourceBlockchains[i].SupportedDestinations = supportedDestinations
		sourceBlockchains[i].AllowedOriginSenderAddresses = allowedAddresses
	}
	destinationsBlockchainIDs := set.NewSet[string](len(supportedDestinations)) // just needs to be non-nil
	for _, id := range supportedDestinations {
		destinationsBlockchainIDs.Add(id)
	}
	sourceBlockchains[0].Validate(&destinationsBlockchainIDs)
	return sourceBlockchains
}

func populateDestinationConfig(allowedAddresses []string) []*config.DestinationBlockchain {
	destinationBlockchain := validDestinationBlockchainConfig
	destinationBlockchain.BlockchainID = id1.String()
	destinationBlockchain.AllowedDestinationAddresses = allowedAddresses
	destinationBlockchain.Validate()
	return []*config.DestinationBlockchain{destinationBlockchain}
}

func makeTestRelayer(t *testing.T, supportedDestinations []string, allowedAddresses []string) *Relayer {
	logger := logging.NewLogger(
		"awm-relayer-test",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	sourceConfig := populateSourceConfig(
		[]ids.ID{
			ids.GenerateTestID(),
		},
		supportedDestinations,
		allowedAddresses,
	)
	destinationConfig := populateDestinationConfig(allowedAddresses)
	return &Relayer{
		sourceBlockchain: *sourceConfig[0],
		logger:           logger,
		globalConfig: &config.Config{
			SourceBlockchains:      sourceConfig,
			DestinationBlockchains: destinationConfig,
		},
	}
}

func TestCheckSupportedDestination(t *testing.T) {
	testCases := []struct {
		name                    string
		supportedDestinations   []string
		destinationBlockchainID ids.ID
		expectedResult          bool
	}{
		{
			name: "explicitly supported destination",
			supportedDestinations: []string{
				id1.String(),
			},
			destinationBlockchainID: id1,
			expectedResult:          true,
		},
		{
			name: "unsupported destination",
			supportedDestinations: []string{
				id1.String(),
			},
			destinationBlockchainID: id2,
			expectedResult:          false,
		},
	}

	for _, testCase := range testCases {
		relayer := makeTestRelayer(t, testCase.supportedDestinations, nil)
		result := relayer.CheckSupportedDestination(testCase.destinationBlockchainID)
		require.Equal(t, testCase.expectedResult, result, fmt.Sprintf("test failed: %s", testCase.name))
	}
}

func TestCheckAllowedAddress(t *testing.T) {
	supportedDestinations := []string{
		id1.String(),
	}
	testCases := []struct {
		name             string
		allowedAddresses []string
		address          common.Address
		expectedResult   bool
	}{
		{
			name: "explicitly allowed address",
			allowedAddresses: []string{
				"0xd81545385803bCD83bd59f58Ba2d2c0562387F83",
			},
			address:        common.HexToAddress("0xd81545385803bCD83bd59f58Ba2d2c0562387F83"),
			expectedResult: true,
		},
		{
			name: "not allowed address",
			allowedAddresses: []string{
				"0x1234567890123456789012345678901234567890",
			},
			address:        common.HexToAddress("0xd81545385803bCD83bd59f58Ba2d2c0562387F83"),
			expectedResult: false,
		},
		{
			name:           "all addresses allowed",
			address:        common.HexToAddress("0xd81545385803bCD83bd59f58Ba2d2c0562387F83"),
			expectedResult: true,
		},
	}

	for _, testCase := range testCases {
		relayer := makeTestRelayer(t, supportedDestinations, testCase.allowedAddresses)

		originResult := relayer.CheckAllowedOriginSenderAddress(testCase.address)
		require.Equal(t, testCase.expectedResult, originResult, fmt.Sprintf("test failed: %s", testCase.name))
		destinationResult := relayer.CheckAllowedDestinationAddress(testCase.address, id1)
		require.Equal(t, testCase.expectedResult, destinationResult, fmt.Sprintf("test failed: %s", testCase.name))
	}
}
