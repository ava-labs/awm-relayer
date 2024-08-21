// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"go.uber.org/zap"
)

const (
	// Max buffer size for ethereum subscription channels
	maxClientSubscriptionBuffer = 20000
	subscribeRetryTimeout       = 1 * time.Second
	MaxBlocksPerRequest         = 200
	rpcMaxRetries               = 5
)

// subscriber implements Subscriber
type subscriber struct {
	wsClient     ethclient.Client
	rpcClient    ethclient.Client
	blockchainID ids.ID
	headers      chan *types.Header
	sub          interfaces.Subscription

	logger logging.Logger
}

// NewSubscriber returns a subscriber
func NewSubscriber(
	logger logging.Logger,
	blockchainID ids.ID,
	wsClient ethclient.Client,
	rpcClient ethclient.Client,
) *subscriber {
	return &subscriber{
		blockchainID: blockchainID,
		wsClient:     wsClient,
		rpcClient:    rpcClient,
		logger:       logger,
		headers:      make(chan *types.Header, maxClientSubscriptionBuffer),
	}
}

// Process logs from the given block height to the latest block. Limits the
// number of blocks retrieved in a single eth_getLogs request to
// `MaxBlocksPerRequest`; if processing more than that, multiple eth_getLogs
// requests will be made.
// Writes true to the done channel when finished, or false if an error occurs
func (s *subscriber) ProcessFromHeight(height *big.Int, done chan bool) {
	defer close(done)
	s.logger.Info(
		"Processing historical logs",
		zap.String("fromBlockHeight", height.String()),
		zap.String("blockchainID", s.blockchainID.String()),
	)
	if height == nil {
		s.logger.Error("Cannot process logs from nil height")
		done <- false
		return
	}

	// Grab the latest block before filtering logs so we don't miss any before updating the db
	latestBlockHeight, err := s.rpcClient.BlockNumber(context.Background())
	if err != nil {
		s.logger.Error(
			"Failed to get latest block",
			zap.String("blockchainID", s.blockchainID.String()),
			zap.Error(err),
		)
		done <- false
		return
	}

	bigLatestBlockHeight := big.NewInt(0).SetUint64(latestBlockHeight)

	//nolint:lll
	for fromBlock := big.NewInt(0).Set(height); fromBlock.Cmp(bigLatestBlockHeight) <= 0; fromBlock.Add(fromBlock, big.NewInt(MaxBlocksPerRequest)) {
		toBlock := big.NewInt(0).Add(fromBlock, big.NewInt(MaxBlocksPerRequest-1))

		// clamp to latest known block because we've already subscribed
		// to new blocks and we don't want to double-process any blocks
		// created after that subscription but before the determination
		// of this "latest"
		if toBlock.Cmp(bigLatestBlockHeight) > 0 {
			toBlock.Set(bigLatestBlockHeight)
		}

		err = s.processBlockRange(fromBlock, toBlock)
		if err != nil {
			s.logger.Error("Failed to process block range", zap.Error(err))
			done <- false
			return
		}
	}
	done <- true
}

// Process Warp messages from the block range [fromBlock, toBlock], inclusive
func (s *subscriber) processBlockRange(
	fromBlock, toBlock *big.Int,
) error {
	for i := fromBlock.Int64(); i <= toBlock.Int64(); i++ {
		header, err := s.getHeaderByNumberRetryable(big.NewInt(i))
		if err != nil {
			s.logger.Error(
				"Failed to get header by number after max attempts",
				zap.String("blockchainID", s.blockchainID.String()),
				zap.Error(err),
			)
			return err
		}
		s.headers <- header
	}
	return nil
}

func (s *subscriber) getHeaderByNumberRetryable(headerNumber *big.Int) (*types.Header, error) {
	var err error
	var header *types.Header
	attempt := 1
	for {
		header, err = s.rpcClient.HeaderByNumber(context.Background(), headerNumber)
		if err == nil {
			return header, nil
		}
		s.logger.Warn(
			"Failed to get header by number",
			zap.String("blockchainID", s.blockchainID.String()),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		if attempt == rpcMaxRetries {
			return nil, err
		}
		time.Sleep(subscribeRetryTimeout)
		attempt++
	}
}

// Loops forever iff maxResubscribeAttempts == 0
func (s *subscriber) Subscribe(maxResubscribeAttempts int) error {
	// Retry subscribing until successful. Attempt to resubscribe maxResubscribeAttempts times
	attempt := 1
	for {
		// Unsubscribe before resubscribing
		// s.sub should only be nil on the first call to Subscribe
		if s.sub != nil {
			s.sub.Unsubscribe()
		}
		err := s.subscribe()
		if err == nil {
			s.logger.Info(
				"Successfully subscribed",
				zap.String("blockchainID", s.blockchainID.String()),
			)
			return nil
		}

		s.logger.Warn(
			"Failed to subscribe to node",
			zap.Int("attempt", attempt),
			zap.String("blockchainID", s.blockchainID.String()),
			zap.Error(err),
		)

		if attempt == maxResubscribeAttempts {
			break
		}

		time.Sleep(subscribeRetryTimeout)
		attempt++
	}

	return fmt.Errorf("failed to subscribe to node with all %d attempts", maxResubscribeAttempts)
}

func (s *subscriber) subscribe() error {
	sub, err := s.wsClient.SubscribeNewHead(context.Background(), s.headers)
	if err != nil {
		s.logger.Error(
			"Failed to subscribe to logs",
			zap.String("blockchainID", s.blockchainID.String()),
			zap.Error(err),
		)
		return err
	}
	s.sub = sub

	return nil
}

func (s *subscriber) Headers() <-chan *types.Header {
	return s.headers
}

func (s *subscriber) Err() <-chan error {
	return s.sub.Err()
}

func (s *subscriber) Cancel() {
	// Nothing to do here, the ethclient manages both the log and err channels
}
