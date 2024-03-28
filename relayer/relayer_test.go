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

func populateSourceConfig(blockchainIDs []ids.ID, supportedDestinations []*config.SupportedDestination, allowedAddresses []string) []*config.SourceBlockchain {
	destinationsBlockchainIDs := set.NewSet[string](len(supportedDestinations)) // just needs to be non-nil
	for _, dest := range supportedDestinations {
		destinationsBlockchainIDs.Add(dest.BlockchainID)
	}

	sourceBlockchains := make([]*config.SourceBlockchain, len(blockchainIDs))
	for i, id := range blockchainIDs {
		sourceBlockchains[i] = &config.TestValidSourceBlockchainConfig
		sourceBlockchains[i].BlockchainID = id.String()
		sourceBlockchains[i].SupportedDestinations = supportedDestinations
		sourceBlockchains[i].AllowedOriginSenderAddresses = allowedAddresses
		sourceBlockchains[i].Validate(&destinationsBlockchainIDs)
	}

	return sourceBlockchains
}

func populateDestinationConfig() []*config.DestinationBlockchain {
	destinationBlockchain := config.TestValidDestinationBlockchainConfig
	destinationBlockchain.BlockchainID = id1.String()
	destinationBlockchain.Validate()
	return []*config.DestinationBlockchain{&destinationBlockchain}
}

func makeTestRelayer(t *testing.T, supportedDestinations []*config.SupportedDestination, allowedAddresses []string) *Relayer {
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
	destinationConfig := populateDestinationConfig()
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
		supportedDestinations   []*config.SupportedDestination
		destinationBlockchainID ids.ID
		expectedResult          bool
	}{
		{
			name: "explicitly supported destination",
			supportedDestinations: []*config.SupportedDestination{
				{
					BlockchainID: id1.String(),
				},
			},
			destinationBlockchainID: id1,
			expectedResult:          true,
		},
		{
			name: "unsupported destination",
			supportedDestinations: []*config.SupportedDestination{
				{
					BlockchainID: id1.String(),
				},
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
	supportedDestination := config.SupportedDestination{
		BlockchainID: id1.String(),
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
		supportedDestination := supportedDestination
		supportedDestination.Addresses = testCase.allowedAddresses
		supportedDestinations := []*config.SupportedDestination{&supportedDestination}
		relayer := makeTestRelayer(t, supportedDestinations, testCase.allowedAddresses)

		originResult := relayer.CheckAllowedOriginSenderAddress(testCase.address)
		require.Equal(t, testCase.expectedResult, originResult, fmt.Sprintf("test failed: %s", testCase.name))
		destinationResult := relayer.CheckAllowedDestinationAddress(testCase.address, id1)
		require.Equal(t, testCase.expectedResult, destinationResult, fmt.Sprintf("test failed: %s", testCase.name))
	}
}
