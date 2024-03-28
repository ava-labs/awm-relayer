package database

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestIsKeyNotFoundError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "key not found error",
			err:      ErrKeyNotFound,
			expected: true,
		},
		{
			name:     "relayer key not found error",
			err:      ErrRelayerIDNotFound,
			expected: true,
		},
		{
			name:     "unknown error",
			err:      errors.New("unknown error"),
			expected: false,
		},
	}
	for _, testCase := range testCases {
		result := IsKeyNotFoundError(testCase.err)
		require.Equal(t, testCase.expected, result, testCase.name)
	}
}

func TestCalculateRelayerID(t *testing.T) {
	id1, _ := ids.FromString("S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD")
	id2, _ := ids.FromString("2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx")
	zeroAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	testCases := []struct {
		name                    string
		sourceBlockchainID      ids.ID
		destinationBlockchainID ids.ID
		originSenderAddress     common.Address
		destinationAddress      common.Address
		expected                common.Hash
	}{
		{
			name:                    "all zero",
			sourceBlockchainID:      id1,
			destinationBlockchainID: id2,
			originSenderAddress:     zeroAddress,
			destinationAddress:      zeroAddress,
			expected:                common.HexToHash("0xf8a8467088fd6f8ad4577408ddda1607e2702ca9827d7fd556c46adae624b7a2"),
		},
		{
			name:                    "zero source address",
			sourceBlockchainID:      id1,
			destinationBlockchainID: id2,
			originSenderAddress:     zeroAddress,
			destinationAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			expected:                common.HexToHash("0xa20ede2231d43d072800ad436a4ca8844f9ddd9cb4174f4cc3046e0958e48320"),
		},
		{
			name:                    "zero destination address",
			sourceBlockchainID:      id1,
			destinationBlockchainID: id2,
			originSenderAddress:     common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			destinationAddress:      zeroAddress,
			expected:                common.HexToHash("0xb205a049831478f55b768a4c875b2085339b6053831ecde8a3d406f9d13454a5"),
		},
		{
			name:                    "all non-zero",
			sourceBlockchainID:      id1,
			destinationBlockchainID: id2,
			originSenderAddress:     common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			destinationAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
			expected:                common.HexToHash("0x6661512bc3b5689b28a4c2519425f725b5681b90fea937433103c846f742f918"),
		},
	}
	for _, testCase := range testCases {
		result := CalculateRelayerID(
			testCase.sourceBlockchainID,
			testCase.destinationBlockchainID,
			testCase.originSenderAddress,
			testCase.destinationAddress,
		)
		require.Equal(t, testCase.expected, result, testCase.name)
	}
}

func TestGetConfigRelayerKeys(t *testing.T) {
	allowedAddress := common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567")
	dstCfg1 := config.TestValidDestinationBlockchainConfig

	// All destination chains and source and destination addresses are allowed
	srcCfg1 := config.TestValidSourceBlockchainConfig

	// All destination chains, but only a single source address is allowed
	srcCfg2 := config.TestValidSourceBlockchainConfig
	srcCfg2.BlockchainID = ids.GenerateTestID().String()
	srcCfg2.AllowedOriginSenderAddresses = []string{allowedAddress.String()}

	// Restricted to a single destination chain, but all source and destination addresses are allowed
	srcCfg3 := config.TestValidSourceBlockchainConfig
	srcCfg3.BlockchainID = ids.GenerateTestID().String()
	srcCfg3.SupportedDestinations = []*config.SupportedDestination{
		{
			BlockchainID: dstCfg1.BlockchainID,
		},
	}

	// Restricted to a single destination chain, but only a single source address is allowed
	srcCfg4 := config.TestValidSourceBlockchainConfig
	srcCfg4.BlockchainID = ids.GenerateTestID().String()
	srcCfg4.AllowedOriginSenderAddresses = []string{allowedAddress.String()}
	srcCfg4.SupportedDestinations = []*config.SupportedDestination{
		{
			BlockchainID: dstCfg1.BlockchainID,
		},
	}

	// Restricted to a single destination chain, but only a single destination address is allowed
	srcCfg5 := config.TestValidSourceBlockchainConfig
	srcCfg5.BlockchainID = ids.GenerateTestID().String()
	srcCfg5.SupportedDestinations = []*config.SupportedDestination{
		{
			BlockchainID: dstCfg1.BlockchainID,
			Addresses:    []string{allowedAddress.String()},
		},
	}

	// Restricted to a single destination, but only a single source and destination address is allowed
	srcCfg6 := config.TestValidSourceBlockchainConfig
	srcCfg6.BlockchainID = ids.GenerateTestID().String()
	srcCfg6.AllowedOriginSenderAddresses = []string{allowedAddress.String()}
	srcCfg6.SupportedDestinations = []*config.SupportedDestination{
		{
			BlockchainID: dstCfg1.BlockchainID,
			Addresses:    []string{allowedAddress.String()},
		},
	}

	//

	err := dstCfg1.Validate()
	require.ErrorIs(t, err, nil)

	allowedDestinations := set.NewSet[string](1)
	allowedDestinations.Add(dstCfg1.BlockchainID)
	err = srcCfg1.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg2.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg3.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg4.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg5.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg6.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)

	cfg := &config.Config{
		SourceBlockchains:      []*config.SourceBlockchain{&srcCfg1, &srcCfg2, &srcCfg3, &srcCfg4, &srcCfg5, &srcCfg6},
		DestinationBlockchains: []*config.DestinationBlockchain{&dstCfg1},
	}

	targetIDs := []RelayerID{
		{
			SourceBlockchainID:      srcCfg1.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     AllAllowedAddress,
			DestinationAddress:      AllAllowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg2.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     allowedAddress,
			DestinationAddress:      AllAllowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg3.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     AllAllowedAddress,
			DestinationAddress:      AllAllowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg4.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     allowedAddress,
			DestinationAddress:      AllAllowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg5.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     AllAllowedAddress,
			DestinationAddress:      allowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg6.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     allowedAddress,
			DestinationAddress:      allowedAddress,
		},
	}

	relayerIDs := GetConfigRelayerIDs(cfg)

	// Test that all target IDs are present
	for i, id := range targetIDs {
		require.True(t,
			func(ids []RelayerID, target RelayerID) bool {
				for _, id := range ids {
					if id.GetID() == target.GetID() {
						return true
					}
				}
				return false
			}(relayerIDs, id),
			fmt.Sprintf("target ID %d not found", i),
		)
	}
}
