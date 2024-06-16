//go:generate mockgen -source=$GOFILE -destination=mocks/mock_inboundmessage.go -package=mocks -mock_names=InboundMessage=MockInboundMessage
package relayer

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	gomessage "github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/relayer/mocks"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type InboundMessage interface {
	message.InboundMessage
}

func TestValidateSignatureResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	message := utils.RandomBytes(1234)
	secretKey, err := bls.NewSecretKey()
	publicKey := bls.PublicFromSecretKey(secretKey)
	require.NoError(t, err)

	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(1, ids.GenerateTestID(), message)
	require.NoError(t, err)

	signatureResponse := msg.SignatureResponse{
		Signature: [96]byte(bls.Sign(secretKey, unsignedMessage.Bytes()).Compress()),
	}
	signatureResponseBytes, err := msg.Codec.Marshal(warp.CodecVersion, signatureResponse)
	require.NoError(t, err)

	t.Run("ResponseFailed", func(t *testing.T) {
		response := mocks.NewMockInboundMessage(ctrl)
		response.EXPECT().Op().Return(gomessage.AppErrorOp)
		_, err = validateSignatureResponse(unsignedMessage, response, publicKey)
		require.ErrorContains(t, err, "relayer async response failed")
	})
	t.Run("Success", func(t *testing.T) {
		response := mocks.NewMockInboundMessage(ctrl)
		response.EXPECT().Op().Return(gomessage.AcceptedOp)
		appResponse := &p2p.AppResponse{
			AppBytes: signatureResponseBytes,
		}
		response.EXPECT().Message().Return(appResponse)
		_, err = validateSignatureResponse(unsignedMessage, response, publicKey)
		require.NoError(t, err)
	})
}

func TestAggregateSignatures(t *testing.T) {
	// ctrl := gomock.NewController(t)
	message := utils.RandomBytes(1234)
	secretKey1, err := bls.NewSecretKey()
	require.NoError(t, err)
	publicKey1 := bls.PublicFromSecretKey(secretKey1)
	secretKey2, err := bls.NewSecretKey()
	require.NoError(t, err)
	publicKey2 := bls.PublicFromSecretKey(secretKey2)
	require.NoError(t, err)

	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(1, ids.GenerateTestID(), message)
	require.NoError(t, err)

	// signature := bls.Sign(secretKey, unsignedMessage.Bytes())
	signature1 := bls.Sign(secretKey1, unsignedMessage.Bytes())
	signature2 := bls.Sign(secretKey2, unsignedMessage.Bytes())

	signatureMap := map[int]blsSignatureBuf{
		1: blsSignatureBuf(signature1.Compress()),
		2: blsSignatureBuf(signature2.Compress()),
	}
	agg, set, err := aggregateSignatures(signatureMap)
	require.NoError(t, err)
	require.True(t, set.Contains(1))
	require.True(t, set.Contains(2))
	aggPublicKeys, err := bls.AggregatePublicKeys([]*bls.PublicKey{publicKey1, publicKey2})
	require.NoError(t, err)
	require.True(t, bls.Verify(aggPublicKeys, agg, unsignedMessage.Bytes()))
}
