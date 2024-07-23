// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer/checkpoint"
	"github.com/ava-labs/awm-relayer/signature-aggregator/aggregator"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/sync/errgroup"

	"go.uber.org/zap"
)

type blsSignatureBuf [bls.SignatureLen]byte

const (
	// Number of retries to collect signatures from validators
	maxRelayerQueryAttempts = 5
	// Maximum amount of time to spend waiting (in addition to network round trip time per attempt)
	// during relayer signature query routine
	signatureRequestRetryWaitPeriodMs = 10_000
)

var (
	codec        = msg.Codec
	coreEthCodec = coreEthMsg.Codec
	// Errors
	errNotEnoughSignatures     = errors.New("failed to collect a threshold of signatures")
	errFailedToGetAggSig       = errors.New("failed to get aggregate signature from node endpoint")
	errNotEnoughConnectedStake = errors.New("failed to connect to a threshold of stake")
)

// ApplicationRelayers define a Warp message route from a specific source address on a specific source blockchain
// to a specific destination address on a specific destination blockchain. This routing information is
// encapsulated in [relayerID], which also represents the database key for an ApplicationRelayer.
type ApplicationRelayer struct {
	logger                    logging.Logger
	metrics                   *ApplicationRelayerMetrics
	network                   *peers.AppRequestNetwork
	sourceBlockchain          config.SourceBlockchain
	signingSubnetID           ids.ID
	destinationClient         vms.DestinationClient
	relayerID                 database.RelayerID
	warpQuorum                config.WarpQuorum
	checkpointManager         *checkpoint.CheckpointManager
	sourceWarpSignatureClient *rpc.Client // nil if configured to fetch signatures via AppRequest for the source blockchain
	signatureAggregator       *aggregator.SignatureAggregator
}

func NewApplicationRelayer(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	network *peers.AppRequestNetwork,
	relayerID database.RelayerID,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	destinationClient vms.DestinationClient,
	sourceBlockchain config.SourceBlockchain,
	startingHeight uint64,
	cfg *config.Config,
	signatureAggregator *aggregator.SignatureAggregator,
) (*ApplicationRelayer, error) {
	quorum, err := cfg.GetWarpQuorum(relayerID.DestinationBlockchainID)
	if err != nil {
		logger.Error(
			"Failed to get warp quorum from config. Relayer may not be configured to deliver to the destination chain.",
			zap.String("destinationBlockchainID", relayerID.DestinationBlockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	var signingSubnet ids.ID
	if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
		// If the message originates from the primary subnet, then we instead "self sign"
		// the message using the validators of the destination subnet.
		signingSubnet = cfg.GetSubnetID(relayerID.DestinationBlockchainID)
	} else {
		// Otherwise, the source subnet signs the message.
		signingSubnet = sourceBlockchain.GetSubnetID()
	}

	sub := ticker.Subscribe()

	checkpointManager := checkpoint.NewCheckpointManager(
		logger,
		db,
		sub,
		relayerID,
		startingHeight,
	)
	checkpointManager.Run()

	var warpClient *rpc.Client
	if !sourceBlockchain.UseAppRequestNetwork() {
		// The subnet-evm Warp API client does not support query parameters or HTTP headers
		// and expects the URI to be in a specific form.
		// Instead, we invoke the Warp API directly via the RPC client.
		warpClient, err = utils.DialWithConfig(
			context.Background(),
			sourceBlockchain.WarpAPIEndpoint.BaseURL,
			sourceBlockchain.WarpAPIEndpoint.HTTPHeaders,
			sourceBlockchain.WarpAPIEndpoint.QueryParams,
		)
		if err != nil {
			logger.Error(
				"Failed to create Warp API client",
				zap.Error(err),
			)
			return nil, err
		}
	}

	ar := ApplicationRelayer{
		logger:                    logger,
		metrics:                   metrics,
		network:                   network,
		sourceBlockchain:          sourceBlockchain,
		destinationClient:         destinationClient,
		relayerID:                 relayerID,
		signingSubnetID:           signingSubnet,
		warpQuorum:                quorum,
		checkpointManager:         checkpointManager,
		sourceWarpSignatureClient: warpClient,
		signatureAggregator:       signatureAggregator,
	}

	return &ar, nil
}

// Process [msgs] at height [height] by relaying each message to the destination chain.
// Checkpoints the height with the checkpoint manager when all messages are relayed.
// ProcessHeight is expected to be called for every block greater than or equal to the
// [startingHeight] provided in the constructor.
func (r *ApplicationRelayer) ProcessHeight(
	height uint64,
	handlers []messages.MessageHandler,
	errChan chan error,
) {
	var eg errgroup.Group
	for _, handler := range handlers {
		// Copy the loop variable to a local variable to avoid the loop variable being captured by the
		// goroutine. Once we upgrade to Go 1.22, we can use the loop variable directly in the goroutine.
		h := handler
		eg.Go(func() error {
			_, err := r.ProcessMessage(h)
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		r.logger.Error(
			"Failed to process block",
			zap.Uint64("height", height),
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Error(err),
		)
		errChan <- err
		return
	}
	r.checkpointManager.StageCommittedHeight(height)
	r.logger.Debug(
		"Processed block",
		zap.Uint64("height", height),
		zap.String("sourceBlockchainID", r.relayerID.SourceBlockchainID.String()),
		zap.String("relayerID", r.relayerID.ID.String()),
		zap.Int("numMessages", len(handlers)),
	)
}

// Relays a message to the destination chain. Does not checkpoint the height.
// returns the transaction hash if the message is successfully relayed.
func (r *ApplicationRelayer) ProcessMessage(handler messages.MessageHandler) (common.Hash, error) {
	r.logger.Debug(
		"Relaying message",
		zap.String("sourceBlockchainID", r.sourceBlockchain.BlockchainID),
		zap.String("relayerID", r.relayerID.ID.String()),
	)
	shouldSend, err := handler.ShouldSendMessage(r.destinationClient)
	if err != nil {
		r.logger.Error(
			"Failed to check if message should be sent",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to check if message should be sent")
		return common.Hash{}, err
	}
	if !shouldSend {
		r.logger.Info("Message should not be sent")
		return common.Hash{}, nil
	}
	unsignedMessage := handler.GetUnsignedMessage()

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	var signedMessage *avalancheWarp.Message

	// sourceWarpSignatureClient is nil iff the source blockchain is configured to fetch signatures via AppRequest
	if r.sourceWarpSignatureClient == nil {
		// TODO: do we actually want to pass the pointer here or adapt the interface?
		signedMessage, err = r.signatureAggregator.AggregateSignaturesAppRequest(unsignedMessage, &r.signingSubnetID, r.warpQuorum.QuorumNumerator)
		r.incFetchSignatureAppRequestCount()
		if err != nil {
			r.logger.Error(
				"Failed to create signed warp message via AppRequest network",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via AppRequest network")
			return common.Hash{}, err
		}
	} else {
		r.incFetchSignatureRPCCount()
		signedMessage, err = r.createSignedMessage(unsignedMessage)
		if err != nil {
			r.logger.Error(
				"Failed to create signed warp message via RPC",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via RPC")
			return common.Hash{}, err
		}
	}

	// create signed message latency (ms)
	r.setCreateSignedMessageLatencyMS(float64(time.Since(startCreateSignedMessageTime).Milliseconds()))

	txHash, err := handler.SendMessage(signedMessage, r.destinationClient)
	if err != nil {
		r.logger.Error(
			"Failed to send warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to send warp message")
		return common.Hash{}, err
	}
	r.logger.Info(
		"Finished relaying message to destination chain",
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		zap.String("txHash", txHash.Hex()),
	)
	r.incSuccessfulRelayMessageCount()

	return txHash, nil
}

func (r *ApplicationRelayer) RelayerID() database.RelayerID {
	return r.relayerID
}

// createSignedMessage fetches the signed Warp message from the source chain via RPC.
// Each VM may implement their own RPC method to construct the aggregate signature, which
// will need to be accounted for here.
func (r *ApplicationRelayer) createSignedMessage(
	unsignedMessage *avalancheWarp.UnsignedMessage,
) (*avalancheWarp.Message, error) {
	r.logger.Info("Fetching aggregate signature from the source chain validators via API")

	var (
		signedWarpMessageBytes hexutil.Bytes
		err                    error
	)
	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		r.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
			zap.String("signingSubnetID", r.signingSubnetID.String()),
		)

		err = r.sourceWarpSignatureClient.CallContext(
			context.Background(),
			&signedWarpMessageBytes,
			"warp_getMessageAggregateSignature",
			unsignedMessage.ID(),
			r.warpQuorum.QuorumNumerator,
			r.signingSubnetID.String(),
		)
		if err == nil {
			warpMsg, err := avalancheWarp.ParseMessage(signedWarpMessageBytes)
			if err != nil {
				r.logger.Error(
					"Failed to parse signed warp message",
					zap.Error(err),
				)
				return nil, err
			}
			return warpMsg, err
		}
		r.logger.Info(
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
	r.logger.Warn(
		"Failed to get aggregate signature from node endpoint",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		zap.String("signingSubnetID", r.signingSubnetID.String()),
	)
	return nil, errFailedToGetAggSig
}

//
// Metrics
//

func (r *ApplicationRelayer) incSuccessfulRelayMessageCount() {
	r.metrics.successfulRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Inc()
}

func (r *ApplicationRelayer) incFailedRelayMessageCount(failureReason string) {
	r.metrics.failedRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String(),
			failureReason).Inc()
}

func (r *ApplicationRelayer) setCreateSignedMessageLatencyMS(latency float64) {
	r.metrics.createSignedMessageLatencyMS.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Set(latency)
}

func (r *ApplicationRelayer) incFetchSignatureRPCCount() {
	r.metrics.fetchSignatureRPCCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Inc()
}

func (r *ApplicationRelayer) incFetchSignatureAppRequestCount() {
	r.metrics.fetchSignatureAppRequestCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Inc()
}
