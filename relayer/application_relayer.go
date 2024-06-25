// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/relayer/checkpoint"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	"golang.org/x/sync/errgroup"

	"go.uber.org/zap"
)

type blsSignatureBuf [bls.SignatureLen]byte

const (
	// Number of retries to collect signatures from validators
	maxRelayerQueryAttempts = 5
	// Maximum amount of time to spend waiting (in addition to network round trip time per attempt) during relayer signature query routine
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
	logger               logging.Logger
	metrics              *ApplicationRelayerMetrics
	sourceBlockchain     config.SourceBlockchain
	destinationClient    vms.DestinationClient
	relayerID            database.RelayerID
	checkpointManager    *checkpoint.CheckpointManager
	currentRequestID     uint32
	messageSignerFactory MessageSignerFactory
	lock                 *sync.RWMutex
}

func NewApplicationRelayer(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	relayerID database.RelayerID,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	destinationClient vms.DestinationClient,
	sourceBlockchain config.SourceBlockchain,
	startingHeight uint64,
	cfg *config.Config,
	messageSignerFactory MessageSignerFactory,
) (*ApplicationRelayer, error) {
	sub := ticker.Subscribe()

	checkpointManager := checkpoint.NewCheckpointManager(logger, db, sub, relayerID, startingHeight)
	checkpointManager.Run()

	ar := ApplicationRelayer{
		logger:               logger,
		metrics:              metrics,
		messageSignerFactory: messageSignerFactory,
		sourceBlockchain:     sourceBlockchain,
		destinationClient:    destinationClient,
		relayerID:            relayerID,
		checkpointManager:    checkpointManager,
		currentRequestID:     rand.Uint32(), // TODONOW: pass via ctor
		lock:                 &sync.RWMutex{},
	}

	return &ar, nil
}

// Process [msgs] at height [height] by relaying each message to the destination chain.
// Checkpoints the height with the checkpoint manager when all messages are relayed.
// ProcessHeight is expected to be called for every block greater than or equal to the [startingHeight] provided in the constructor
func (r *ApplicationRelayer) ProcessHeight(height uint64, handlers []messages.MessageHandler, errChan chan error) {
	var eg errgroup.Group
	for _, handler := range handlers {
		// Copy the loop variable to a local variable to avoid the loop variable being captured by the goroutine
		// Once we upgrade to Go 1.22, we can use the loop variable directly in the goroutine
		h := handler
		eg.Go(func() error {
			return r.ProcessMessage(h)
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
func (r *ApplicationRelayer) ProcessMessage(handler messages.MessageHandler) error {
	// Increment the request ID. Make sure we don't hold the lock while we relay the message.
	r.lock.Lock()
	r.currentRequestID++
	reqID := r.currentRequestID
	r.lock.Unlock()

	err := r.relayMessage(
		reqID,
		handler,
		true,
		r.messageSignerFactory,
	)

	return err
}

func (r *ApplicationRelayer) RelayerID() database.RelayerID {
	return r.relayerID
}

func (r *ApplicationRelayer) relayMessage(
	requestID uint32,
	handler messages.MessageHandler,
	useAppRequestNetwork bool,
	messageSignerFactory MessageSignerFactory,
) error {
	r.logger.Debug(
		"Relaying message",
		zap.Uint32("requestID", requestID),
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
		return err
	}
	if !shouldSend {
		r.logger.Info("Message should not be sent")
		return nil
	}
	unsignedMessage := handler.GetUnsignedMessage()

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	signedMessage, err := messageSignerFactory(requestID).SignMessage(unsignedMessage)
	if err != nil {
		r.logger.Error(
			"Failed to create signed warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to create signed warp message")
		return err
	}

	// create signed message latency (ms)
	r.setCreateSignedMessageLatencyMS(float64(time.Since(startCreateSignedMessageTime).Milliseconds()))

	err = handler.SendMessage(signedMessage, r.destinationClient)
	if err != nil {
		r.logger.Error(
			"Failed to send warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to send warp message")
		return err
	}
	r.logger.Info(
		"Finished relaying message to destination chain",
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
	)
	r.incSuccessfulRelayMessageCount()

	return nil
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
