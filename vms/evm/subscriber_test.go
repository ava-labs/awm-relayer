package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	mock_ethclient "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func makeSubscriberWithMockEthClient(t *testing.T) (*subscriber, *mock_ethclient.MockClient) {
	sourceSubnet := config.SourceSubnet{
		SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
		BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
		VM:           config.EVM.String(),
		RPCEndpoint:  "https://subnets.avax.network/mysubnet/rpc",
	}

	logger := logging.NoLog{}

	mockEthClient := mock_ethclient.NewMockClient(gomock.NewController(t))
	subscriber := NewSubscriber(logger, sourceSubnet)
	subscriber.dial = func(_url string) (ethclient.Client, error) { return mockEthClient, nil }

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

	expectFilterLogs := func(
		mock *mock_ethclient.MockClient,
		fromBlock int64,
		toBlock int64,
	) {
		mock.EXPECT().FilterLogs(
			gomock.Any(),
			gomock.Eq(interfaces.FilterQuery{
				Topics:    warpFilterQuery.Topics,
				Addresses: warpFilterQuery.Addresses,
				FromBlock: big.NewInt(fromBlock),
				ToBlock:   big.NewInt(toBlock),
			}),
		).Return([]types.Log{}, nil).Times(1)
	}

	// TODO: switch to the built-in min() when we get to Go 1.21
	min := func(a, b int64) int64 {
		if a < b {
			return a
		} else {
			return b
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subscriberUnderTest, mockEthClient := makeSubscriberWithMockEthClient(t)

			mockEthClient.
				EXPECT().
				BlockNumber(gomock.Any()).
				Return(uint64(tc.latest), nil).
				Times(1)

			for i := tc.input; i <= tc.latest; i += MaxBlocksPerRequest {
				expectFilterLogs(
					mockEthClient,
					i,
					min(i+MaxBlocksPerRequest-1, tc.latest),
				)
			}
			done := make(chan bool, 1)
			subscriberUnderTest.ProcessFromHeight(big.NewInt(tc.input), done)
			result := <-done
			require.True(t, result)
			close(done)
		})
	}
}
