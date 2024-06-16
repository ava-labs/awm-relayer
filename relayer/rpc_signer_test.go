//go:generate mockgen -source=$GOFILE -destination=mocks/mock_warpbackend.go -package=mocks -mock_names=WarpBackendClient=MockWarpBackendClient

package relayer

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/relayer/mocks"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type WarpClient interface {
	warpBackend.Client
}

func TestRPCSigner(t *testing.T) {
	ctrl := gomock.NewController(t)
	message := utils.RandomBytes(1234)
	secretKey, err := bls.NewSecretKey()
	publicKey := bls.PublicFromSecretKey(secretKey)
	_ = publicKey
	require.NoError(t, err)

	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(1, ids.GenerateTestID(), message)
	require.NoError(t, err)
	// srcSubnetID := ids.GenerateTestID()
	srcBlockchainID := ids.GenerateTestID()
	destSubnetID := ids.GenerateTestID()
	destBlockchainID := ids.GenerateTestID()
	var fn WarpBackendNewClient = func(uri, chain string) (warpBackend.Client, error) {
		return nil, fmt.Errorf("not implemented")
	}
	rpcMessageSigner := RPCMessageSigner{
		warpQuorumNumerator:        10,
		signingSubnetID:            destSubnetID,
		sourceBlockchainID:         srcBlockchainID,
		sourceBlockchainRPCBaseURL: "http://myrpc.com/endpoint",
		logger:                     logging.NoLog{},
		destinationBlockchainID:    destBlockchainID,
		warpBackendNewClient:       fn,
		delayBeforeRetry:           1 * time.Microsecond,
	}
	_ = rpcMessageSigner

	t.Run("FailedNewClient", func(t *testing.T) {
		warpClient := mocks.NewMockWarpClient(ctrl)
		_ = warpClient
		rpcMessageSigner.warpBackendNewClient = func(uri, chain string) (warpBackend.Client, error) {
			return nil, fmt.Errorf("failed to create new client")
		}
		signedMessage, err := rpcMessageSigner.SignMessage(unsignedMessage)
		require.ErrorContains(t, err, "failed to create new client")
		require.Nil(t, signedMessage)
	})
	t.Run("FailedGetMessageAggregateSignature", func(t *testing.T) {
		warpClient := mocks.NewMockWarpClient(ctrl)
		warpClient.EXPECT().GetMessageAggregateSignature(gomock.Any(), unsignedMessage.ID(), uint64(10), destSubnetID.String()).Return(nil, fmt.Errorf("failed signature aggregation")).Times(5)
		rpcMessageSigner.warpBackendNewClient = func(uri, chain string) (warpBackend.Client, error) {
			return warpClient, nil
		}
		signedMessage, err := rpcMessageSigner.SignMessage(unsignedMessage)
		require.ErrorIs(t, err, errFailedToGetAggSig)
		require.Nil(t, signedMessage)
	})
}
