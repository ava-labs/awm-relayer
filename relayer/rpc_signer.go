package relayer

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
	"go.uber.org/zap"
)

func NewRPCMessageSigner(
	warpQuorum config.WarpQuorum,
	srcBlockchain config.SourceBlockchain,
	destBlockchain config.DestinationBlockchain,
	logger logging.Logger,
) *RPCMessageSigner {
	return &RPCMessageSigner{
		warpQuorumNumerator:        warpQuorum.QuorumNumerator,
		signingSubnetID:            srcBlockchain.GetSubnetID(),
		sourceBlockchainID:         srcBlockchain.GetBlockchainID(),
		sourceBlockchainRPCBaseURL: srcBlockchain.RPCEndpoint.BaseURL,
		destinationBlockchainID:    destBlockchain.GetBlockchainID(),
		logger:                     logger,
		warpBackendNewClient:       warpBackend.NewClient,
	}
}

type RPCMessageSigner struct {
	warpQuorumNumerator        uint64
	signingSubnetID            ids.ID
	sourceBlockchainID         ids.ID
	sourceBlockchainRPCBaseURL string
	// warpBackendNewClient is a private field for testing purpose
	warpBackendNewClient func(uri, chain string) (warpBackend.Client, error)
	// logging purpose
	logger                  logging.Logger
	destinationBlockchainID ids.ID
}

func (s *RPCMessageSigner) Sign(unsignedMessage *avalancheWarp.UnsignedMessage) (*avalancheWarp.Message, error) {
	s.logger.Info("Fetching aggregate signature from the source chain validators via API")
	// TODO: To properly support this, we should provide a dedicated Warp API endpoint in the config
	uri := utils.StripFromString(s.sourceBlockchainRPCBaseURL, "/ext")
	warpClient, err := warpBackend.NewClient(uri, s.sourceBlockchainID.String())
	if err != nil {
		s.logger.Error(
			"Failed to create Warp API client",
			zap.Error(err),
		)
		return nil, err
	}

	var signedWarpMessageBytes []byte
	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		s.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", s.sourceBlockchainID.String()),
			zap.String("destinationBlockchainID", s.destinationBlockchainID.String()),
			zap.String("signingSubnetID", s.signingSubnetID.String()),
		)
		signedWarpMessageBytes, err = warpClient.GetMessageAggregateSignature(
			context.Background(),
			unsignedMessage.ID(),
			s.warpQuorumNumerator,
			s.signingSubnetID.String(),
		)
		if err == nil {
			warpMsg, err := avalancheWarp.ParseMessage(signedWarpMessageBytes)
			if err != nil {
				s.logger.Error(
					"Failed to parse signed warp message",
					zap.Error(err),
				)
				return nil, err
			}
			return warpMsg, err
		}
		s.logger.Info(
			"Failed to get aggregate signature from node endpoint. Retrying.",
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		if attempt != maxRelayerQueryAttempts {
			// Sleep such that all retries are uniformly spread across totalRelayerQueryPeriodMs
			// TODO: We may want to consider an exponential back off rather than a uniform sleep period.
			time.Sleep(time.Duration(signatureRequestRetryWaitPeriodMs/maxRelayerQueryAttempts) * time.Millisecond)
		}
	}
	s.logger.Warn(
		"Failed to get aggregate signature from node endpoint",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("sourceBlockchainID", s.sourceBlockchainID.String()),
		zap.String("destinationBlockchainID", s.destinationBlockchainID.String()),
		zap.String("signingSubnetID", s.signingSubnetID.String()),
	)
	return nil, errFailedToGetAggSig
}
