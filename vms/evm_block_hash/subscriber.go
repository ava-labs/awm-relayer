package evm_block_hash

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/warp/payload"
	"go.uber.org/zap"
)

const (
	// Max buffer size for ethereum subscription channels
	maxClientSubscriptionBuffer = 20000
	subscribeRetryTimeout       = 1 * time.Second
	maxResubscribeAttempts      = 10
)

var (
	// Errors
	ErrInvalidLog = errors.New("invalid warp block hash log")
)

// subscriber implements Subscriber
type subscriber struct {
	nodeWSURL  string
	nodeRPCURL string
	chainID    ids.ID
	logsChan   chan vmtypes.WarpMessageInfo
	blocks     <-chan *types.Header
	sub        interfaces.Subscription
	networkID  uint32

	logger logging.Logger
	db     database.RelayerDatabase
}

// NewSubscriber returns a subscriber
func NewSubscriber(logger logging.Logger, subnetInfo config.SourceSubnet, db database.RelayerDatabase) *subscriber {
	chainID, err := ids.FromString(subnetInfo.ChainID)
	if err != nil {
		logger.Error(
			"Invalid chainID provided to subscriber",
			zap.Error(err),
		)
		return nil
	}

	logs := make(chan vmtypes.WarpMessageInfo, maxClientSubscriptionBuffer)

	return &subscriber{
		nodeWSURL:  subnetInfo.GetNodeWSEndpoint(),
		nodeRPCURL: subnetInfo.GetNodeRPCEndpoint(),
		chainID:    chainID,
		logger:     logger,
		db:         db,
		logsChan:   logs,
		networkID:  config.GetNetworkID(),
	}
}

func (s *subscriber) Subscribe() error {
	// Retry subscribing until successful. Attempt to resubscribe maxResubscribeAttempts times
	for attempt := 0; attempt < maxResubscribeAttempts; attempt++ {
		// Unsubscribe before resubscribing
		// s.sub should only be nil on the first call to Subscribe
		if s.sub != nil {
			s.sub.Unsubscribe()
		}
		err := s.dialAndSubscribe()
		if err == nil {
			s.logger.Info(
				"Successfully subscribed",
				zap.String("chainID", s.chainID.String()),
			)
			return nil
		}

		s.logger.Warn(
			"Failed to subscribe to node",
			zap.Int("attempt", attempt),
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)

		if attempt != maxResubscribeAttempts-1 {
			time.Sleep(subscribeRetryTimeout)
		}
	}

	return fmt.Errorf("failed to subscribe to node with all %d attempts", maxResubscribeAttempts)
}

func (s *subscriber) dialAndSubscribe() error {
	// Dial the configured source chain endpoint
	// This needs to be a websocket
	ethClient, err := ethclient.Dial(s.nodeWSURL)
	if err != nil {
		return err
	}

	blocks := make(chan *types.Header, maxClientSubscriptionBuffer)
	sub, err := ethClient.SubscribeNewHead(context.Background(), blocks)
	if err != nil {
		s.logger.Error(
			"Failed to subscribe to logs",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}
	s.blocks = blocks
	s.sub = sub

	// Forward logs to the interface channel. Closed when the subscription is cancelled
	go s.forwardLogs()
	return nil
}

func (s *subscriber) NewWarpMessageInfo(block *types.Header) (*vmtypes.WarpMessageInfo, error) {
	blockHashPayload, err := payload.NewBlockHashPayload(block.Hash())
	if err != nil {
		return nil, err
	}
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(s.networkID, s.chainID, blockHashPayload.Bytes())
	if err != nil {
		return nil, err
	}
	err = unsignedMessage.Initialize()
	if err != nil {
		return nil, err
	}

	return &vmtypes.WarpMessageInfo{
		UnsignedMsgBytes: unsignedMessage.Bytes(),
		BlockNumber:      block.Number.Uint64(),
		BlockTimestamp:   block.Time,
	}, nil
}

// forward logs from the concrete log channel to the interface channel
func (s *subscriber) forwardLogs() {
	// TODO: When publishing anycast messages from the C-Chain, we need to create
	// a separate aggregate signature for each destination chain.
	// The easiest thing to do would be to fan out here and produce new logs for each of
	// the destinations, but that may introduce a cyclic dependency between this package
	// and the anycast message protocol package.
	for block := range s.blocks {
		messageInfo, err := s.NewWarpMessageInfo(block)
		if err != nil {
			s.logger.Error(
				"Invalid log. Continuing.",
				zap.Error(err),
			)
			continue
		}
		s.logsChan <- *messageInfo
	}
}

func (s *subscriber) ProcessFromHeight(height *big.Int) error {
	// TODO: Implement historical block processing
	return nil
}

func (s *subscriber) SetProcessedBlockHeightToLatest() error {
	// TODO: Implement historical block processing
	// We should distinguish the key from the value for the evm relayer: chainID_blockhash
	return nil
}

func (s *subscriber) Logs() <-chan vmtypes.WarpMessageInfo {
	return s.logsChan
}

func (s *subscriber) Err() <-chan error {
	return s.sub.Err()
}

func (s *subscriber) Cancel() {
	// Nothing to do here, the ethclient manages both the log and err channels
}
