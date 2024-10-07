// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/awm-relayer/peers/validators"
	"go.uber.org/zap"
)

var errEmptyProposerHeightCache = errors.New("empty proposer height cache")

const pChainLookback = 30 * time.Second

type ProposerHeightCache struct {
	logger           logging.Logger
	pChainClient     validators.CanonicalValidatorClient
	timeToHeight     *linked.Hashmap[time.Time, uint64]
	updateInterval   time.Duration
	timeToHeightLock sync.Mutex
}

func newProposerHeightCache(
	logger logging.Logger,
	pChainClient validators.CanonicalValidatorClient,
	updateInterval time.Duration,
) *ProposerHeightCache {
	pHeightCache := &ProposerHeightCache{
		logger:         logger,
		pChainClient:   pChainClient,
		timeToHeight:   linked.NewHashmap[time.Time, uint64](),
		updateInterval: updateInterval,
	}
	pHeightCache.updateData()
	return pHeightCache
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
	p.timeToHeightLock.Lock()
	defer p.timeToHeightLock.Unlock()

	height, err := p.pChainClient.GetCurrentHeight(context.Background())
	if err != nil {
		p.logger.Warn("Failed to get P-Chain height", zap.Error(err))
		return
	}
	// If the newest height is already in the cache, no need to update
	_, newestHeight, ok := p.timeToHeight.Newest()
	if ok && newestHeight == height {
		return
	}

	// Clean up any heights outside of the lookback period
	it := p.timeToHeight.NewIterator()
	for it.Next() {
		if time.Since(it.Key()) > pChainLookback {
			p.timeToHeight.Delete(it.Key())
		}
	}

	blockBytes, err := p.pChainClient.GetBlockByHeight(context.Background(), height)
	if err != nil {
		p.logger.Warn("Failed to get P-Chain block by height", zap.Error(err))
		return
	}

	parsedBlock, err := block.Parse(block.Codec, blockBytes)
	if err != nil {
		p.logger.Warn("failed to parse platformvm block", zap.Error(err))
		return
	}
	// Convert to banff block to get access to Timestamp method
	banffBlock, ok := parsedBlock.(*block.BanffStandardBlock)
	if !ok {
		p.logger.Warn("failed to convert to banff block")
		return
	}
	banffBlockTime := banffBlock.Timestamp()
	p.timeToHeight.Put(banffBlockTime, height)

}

// GetOptimalHeight returns a best guess for a proposerVM height of the P-chain
// using the most recent block time that is at least pChainLookback in the past.
func (p *ProposerHeightCache) GetOptimalHeight() (uint64, error) {
	p.timeToHeightLock.Lock()
	defer p.timeToHeightLock.Unlock()

	if p.timeToHeight.Len() == 0 {
		return 0, errEmptyProposerHeightCache
	}
	// declaring variables outside of loop to have access to them after the loop
	// is finished
	var t time.Time
	var height uint64

	it := p.timeToHeight.NewIterator()
	for it.Next() {
		t, height = it.Key(), it.Value()
		if time.Since(t) < pChainLookback {
			return height - 1, nil
		}
	}

	// If this is reached the cache only contains entries older than pChainLookback
	// so we return the parent of the most recent height
	return height - 1, nil
}
