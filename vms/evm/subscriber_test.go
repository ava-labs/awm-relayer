// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	mock_ethclient "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func makeSubscriberWithMockEthClient(t *testing.T) (*subscriber, *mock_ethclient.MockClient) {
	sourceSubnet := config.SourceBlockchain{
		SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
		BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
		VM:           config.EVM.String(),
		RPCEndpoint:  "https://subnets.avax.network/mysubnet/rpc",
	}

	logger := logging.NoLog{}

	mockEthClient := mock_ethclient.NewMockClient(gomock.NewController(t))
	blockchainID, err := ids.FromString(sourceSubnet.BlockchainID)
	require.NoError(t, err)
	subscriber := NewSubscriber(logger, blockchainID, mockEthClient)

	return subscriber, mockEthClient
}

func TestProcessFromHeight(t *testing.T) {
	testCases := []struct {
		name   string
		latest int64
		input  int64
	}{
		{
			name:   "zero to max blocks",
			latest: 200,
			input:  0,
		},
		{
			name:   "max blocks",
			latest: 1000,
			input:  800,
		},
		{
			name:   "greater than max blocks",
			latest: 1000,
			input:  700,
		},
		{
			name:   "many rounds greater than max blocks",
			latest: 19642,
			input:  751,
		},
		{
			name:   "latest is less than max blocks",
			latest: 96,
			input:  41,
		},
		{
			name:   "invalid starting block number",
			latest: 50,
			input:  51,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subscriberUnderTest, mockEthClient := makeSubscriberWithMockEthClient(t)

			mockEthClient.
				EXPECT().
				BlockNumber(gomock.Any()).
				Return(uint64(tc.latest), nil).
				Times(1)

			for i := tc.input; i <= tc.latest; i++ {
				mockEthClient.EXPECT().HeaderByNumber(
					gomock.Any(),
					big.NewInt(i),
				).Return(&types.Header{
					Number: big.NewInt(i),
				}, nil).Times(1)
			}
			err := subscriberUnderTest.ProcessFromHeight(big.NewInt(tc.input))
			require.NoError(t, err)
		})
	}
}
