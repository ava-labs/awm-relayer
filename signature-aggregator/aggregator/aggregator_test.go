package aggregator

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/peers/mocks"
	"github.com/ava-labs/awm-relayer/signature-aggregator/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var sigAggMetrics *metrics.SignatureAggregatorMetrics
var messageCreator message.Creator

func instantiateAggregator(t *testing.T) (
	*SignatureAggregator,
	*mocks.MockAppRequestNetwork,
) {
	mockNetwork := mocks.NewMockAppRequestNetwork(gomock.NewController(t))
	if sigAggMetrics == nil {
		sigAggMetrics = metrics.NewSignatureAggregatorMetrics(prometheus.DefaultRegisterer)
	}
	if messageCreator == nil {
		var err error
		messageCreator, err = message.NewCreator(
			logging.NoLog{},
			prometheus.DefaultRegisterer,
			constants.DefaultNetworkCompressionType,
			constants.DefaultNetworkMaximumInboundTimeout,
		)
		require.Equal(t, err, nil)
	}
	aggregator, err := NewSignatureAggregator(
		mockNetwork,
		logging.NoLog{},
		1024,
		sigAggMetrics,
		messageCreator,
		// Setting the etnaTime to a minute ago so that the post-etna code path is used in the test
		time.Now().Add(-1*time.Minute),
	)
	require.Equal(t, err, nil)
	return aggregator, mockNetwork
}

func TestCreateSignedMessageFailsWithNoValidators(t *testing.T) {
	aggregator, mockNetwork := instantiateAggregator(t)
	msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
	require.Equal(t, err, nil)
	mockNetwork.EXPECT().GetSubnetID(ids.Empty).Return(ids.Empty, nil)
	mockNetwork.EXPECT().ConnectToCanonicalValidators(ids.Empty).Return(
		&peers.ConnectedCanonicalValidators{
			ConnectedWeight:      0,
			TotalValidatorWeight: 0,
			ValidatorSet:         []*warp.Validator{},
		},
		nil,
	)
	_, err = aggregator.CreateSignedMessage(msg, nil, ids.Empty, 80)
	require.ErrorContains(t, err, "no signatures")
}

func TestCreateSignedMessageFailsWithoutSufficientConnectedStake(t *testing.T) {
	aggregator, mockNetwork := instantiateAggregator(t)
	msg, err := warp.NewUnsignedMessage(0, ids.Empty, []byte{})
	require.Equal(t, err, nil)
	mockNetwork.EXPECT().GetSubnetID(ids.Empty).Return(ids.Empty, nil)
	mockNetwork.EXPECT().ConnectToCanonicalValidators(ids.Empty).Return(
		&peers.ConnectedCanonicalValidators{
			ConnectedWeight:      0,
			TotalValidatorWeight: 1,
			ValidatorSet:         []*warp.Validator{},
		},
		nil,
	)
	_, err = aggregator.CreateSignedMessage(msg, nil, ids.Empty, 80)
	require.ErrorContains(
		t,
		err,
		"failed to connect to a threshold of stake",
	)
}
