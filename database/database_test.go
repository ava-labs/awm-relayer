package database

import (
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
	// Create a configuration with two source and two destination blockchains.
	// One of which allows all addresses, the other only a specific address.
	srcCfg1 := config.TestValidSourceBlockchainConfig
	srcCfg2 := config.TestValidSourceBlockchainConfig
	srcCfg2.BlockchainID = ids.GenerateTestID().String()
	srcCfg2.AllowedOriginSenderAddresses = []string{allowedAddress.String()}

	dstCfg1 := config.TestValidDestinationBlockchainConfig
	dstCfg2 := config.TestValidDestinationBlockchainConfig
	dstCfg2.BlockchainID = ids.GenerateTestID().String()
	dstCfg2.AllowedDestinationAddresses = []string{allowedAddress.String()}

	allowedDestinations := set.NewSet[string](2)
	allowedDestinations.Add(dstCfg1.BlockchainID)
	allowedDestinations.Add(dstCfg2.BlockchainID)
	err := srcCfg1.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = srcCfg2.Validate(&allowedDestinations)
	require.ErrorIs(t, err, nil)
	err = dstCfg1.Validate()
	require.ErrorIs(t, err, nil)
	err = dstCfg2.Validate()
	require.ErrorIs(t, err, nil)

	cfg := &config.Config{
		SourceBlockchains:      []*config.SourceBlockchain{&srcCfg1, &srcCfg2},
		DestinationBlockchains: []*config.DestinationBlockchain{&dstCfg1, &dstCfg2},
	}

	targetIDs := []RelayerID{
		{
			SourceBlockchainID:      srcCfg1.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     common.Address{},
			DestinationAddress:      common.Address{},
		},
		{
			SourceBlockchainID:      srcCfg1.GetBlockchainID(),
			DestinationBlockchainID: dstCfg2.GetBlockchainID(),
			OriginSenderAddress:     common.Address{},
			DestinationAddress:      allowedAddress,
		},
		{
			SourceBlockchainID:      srcCfg2.GetBlockchainID(),
			DestinationBlockchainID: dstCfg1.GetBlockchainID(),
			OriginSenderAddress:     allowedAddress,
			DestinationAddress:      common.Address{},
		},
		{
			SourceBlockchainID:      srcCfg2.GetBlockchainID(),
			DestinationBlockchainID: dstCfg2.GetBlockchainID(),
			OriginSenderAddress:     allowedAddress,
			DestinationAddress:      allowedAddress,
		},
	}

	relayerKeys := GetConfigRelayerIDs(cfg)

	for _, key := range targetIDs {
		require.True(t, func(keys []RelayerID, target RelayerID) bool {
			for _, key := range keys {
				if key.GetID() == target.GetID() {
					return true
				}
			}
			return false
		}(relayerKeys, key))
	}
}
