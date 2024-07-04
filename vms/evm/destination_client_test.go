// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	mock_ethclient "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	"github.com/ava-labs/awm-relayer/vms/evm/signer"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var destinationSubnet = config.DestinationBlockchain{
	SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
	BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
	VM:           config.EVM.String(),
	RPCEndpoint: config.APIConfig{
		BaseURL: "https://subnets.avax.network/mysubnet/rpc",
	},
	AccountPrivateKey: "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
}

func TestSendTx(t *testing.T) {
	txSigner, err := signer.NewTxSigner(destinationSubnet.AccountPrivateKey)
	require.NoError(t, err)

	testError := fmt.Errorf("call errored")
	testCases := []struct {
		name                  string
		chainIDErr            error
		chainIDTimes          int
		estimateBaseFeeErr    error
		estimateBaseFeeTimes  int
		suggestGasTipCapErr   error
		suggestGasTipCapTimes int
		sendTransactionErr    error
		sendTransactionTimes  int
		expectError           bool
	}{
		{
			name:                  "valid",
			chainIDTimes:          1,
			estimateBaseFeeTimes:  1,
			suggestGasTipCapTimes: 1,
			sendTransactionTimes:  1,
		},
		{
			name:                 "invalid estimateBaseFee",
			estimateBaseFeeErr:   testError,
			estimateBaseFeeTimes: 1,
			expectError:          true,
		},
		{
			name:                  "invalid suggestGasTipCap",
			estimateBaseFeeTimes:  1,
			suggestGasTipCapErr:   testError,
			suggestGasTipCapTimes: 1,
			expectError:           true,
		},
		{
			name:                  "invalid sendTransaction",
			chainIDTimes:          1,
			estimateBaseFeeTimes:  1,
			suggestGasTipCapTimes: 1,
			sendTransactionErr:    testError,
			sendTransactionTimes:  1,
			expectError:           true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockClient := mock_ethclient.NewMockClient(ctrl)
			destinationClient := &destinationClient{
				lock:       &sync.Mutex{},
				logger:     logging.NoLog{},
				client:     mockClient,
				evmChainID: big.NewInt(5),
				signer:     txSigner,
			}
			warpMsg := &avalancheWarp.Message{}
			toAddress := "0x27aE10273D17Cd7e80de8580A51f476960626e5f"

			gomock.InOrder(
				mockClient.EXPECT().EstimateBaseFee(gomock.Any()).Return(
					new(big.Int),
					test.estimateBaseFeeErr,
				).Times(test.estimateBaseFeeTimes),
				mockClient.EXPECT().SuggestGasTipCap(gomock.Any()).Return(
					new(big.Int),
					test.suggestGasTipCapErr,
				).Times(test.suggestGasTipCapTimes),
				mockClient.EXPECT().SendTransaction(gomock.Any(), gomock.Any()).Return(
					test.sendTransactionErr,
				).Times(test.sendTransactionTimes),
			)

			_, err := destinationClient.SendTx(warpMsg, toAddress, 0, []byte{})
			if test.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
