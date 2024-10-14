// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/awm-relayer/peers/validators"
	"go.uber.org/zap"
)

var (
	errFailedToGetCurrentHeight = errors.New("failed to get current P-chain height")
)

const pChainLookback = 30 * time.Second

type ProposerHeightCache struct {
	logger       logging.Logger
	pChainClient validators.CanonicalValidatorClient
	// protected by timeToHeightLock
	timeToHeight     *linked.Hashmap[time.Time, uint64]
	updateInterval   time.Duration
	timeToHeightLock sync.RWMutex

	// value kept separately since we might end up with an empty cache if there have been
	// no new blocks within the pChainLookback period needs to be accessed via [atomic]
	currentMaxHeight uint64
}

func NewProposerHeightCache(
	logger logging.Logger,
	pChainClient validators.CanonicalValidatorClient,
	updateInterval time.Duration,
) (*ProposerHeightCache, error) {
	pHeightCache := &ProposerHeightCache{
		logger:         logger,
		pChainClient:   pChainClient,
		timeToHeight:   linked.NewHashmap[time.Time, uint64](),
		updateInterval: updateInterval,
	}
	// Initialize [currentMaxHeight] value
	currentHeight, err := pChainClient.GetCurrentHeight(context.Background())
	if err != nil {
		return nil, errFailedToGetCurrentHeight
	}
	atomic.StoreUint64(&pHeightCache.currentMaxHeight, currentHeight)
	return pHeightCache, nil
}

func (p *ProposerHeightCache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.updateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.updateData()
			case <-ctx.Done():
				p.logger.Info("proposerHeightCache shutting down, context done")
				return
			}
		}
	}()
}

func (p *ProposerHeightCache) updateData() {
	height, err := p.pChainClient.GetCurrentHeight(context.Background())
	if err != nil {
		p.logger.Warn("Failed to get P-Chain height", zap.Error(err))
		return
	}
	// If currentMaxHeight is already in the cache, no need to update
	// or evict old entries.
	currentMaxHeight := atomic.LoadUint64(&p.currentMaxHeight)
	if currentMaxHeight == height {
		return
	}

	for i := currentMaxHeight + 1; i <= height; i++ {
		err := p.writeTimeForHeight(i)
		if err != nil {
			// Log the warning and continue
			p.logger.Warn("Failed to write time for height", zap.Uint64("height", i), zap.Error(err))
		}
	}

	p.evictExpired()
}

func (p *ProposerHeightCache) evictExpired() {
	p.timeToHeightLock.Lock()
	defer p.timeToHeightLock.Unlock()

	it := p.timeToHeight.NewIterator()
	for it.Next() {
		if time.Since(it.Key()) > pChainLookback {
			p.timeToHeight.Delete(it.Key())
		} else {
			// The cache will be strictly increasing in time, so we can break as soon as we find a time that is not too old
			return
		}
	}
}

func (p *ProposerHeightCache) writeTimeForHeight(height uint64) error {
	p.timeToHeightLock.Lock()
	defer p.timeToHeightLock.Unlock()
	blockBytes, err := p.pChainClient.GetBlockByHeight(context.Background(), height)
	if err != nil {
		p.logger.Warn("Failed to get P-Chain block by height", zap.Error(err))
		return err
	}

	parsedBlock, err := block.Parse(block.Codec, blockBytes)
	if err != nil {
		p.logger.Warn("failed to parse platformvm block", zap.Error(err))
		return err
	}
	// Convert to banff block to get access to Timestamp method
	banffBlock, ok := parsedBlock.(*block.BanffStandardBlock)
	if !ok {
		p.logger.Warn("failed to convert to banff block")
		return err
	}
	banffBlockTime := banffBlock.Timestamp()

	p.timeToHeight.Put(banffBlockTime, height)
	atomic.StoreUint64(&p.currentMaxHeight, height)
	return nil
}

// GetOptimalHeight returns a best guess for a proposerVM height of the P-chain
// using the most recent block time that is at least pChainLookback in the past.
func (p *ProposerHeightCache) GetOptimalHeight() uint64 {
	p.timeToHeightLock.RLock()
	defer p.timeToHeightLock.RUnlock()

	if p.timeToHeight.Len() == 0 {
		return atomic.LoadUint64(&p.currentMaxHeight) - 1
	}

	it := p.timeToHeight.NewIterator()
	for it.Next() {
		t, height := it.Key(), it.Value()
		if time.Since(t) < pChainLookback {
			return height - 1
		}
	}

	// If this is reached the cache only contains entries older than pChainLookback
	// so we return the parent of the most recent height
	return atomic.LoadUint64(&p.currentMaxHeight) - 1
}
