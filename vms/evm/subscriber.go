// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	// Max buffer size for ethereum subscription channels
	maxClientSubscriptionBuffer = 20000
	subscribeRetryTimeout       = 1 * time.Second
	maxResubscribeAttempts      = 10
	MaxBlocksToProcess          = 200
)

var (
	// Errors
	ErrInvalidLog = errors.New("invalid warp message log")
)

// The filter query used to match logs emitted by the Warp precompile
var warpFilterQuery = interfaces.FilterQuery{
	Topics: [][]common.Hash{
		{warp.WarpABI.Events["SendWarpMessage"].ID},
		{},
		{},
	},
	Addresses: []common.Address{
		warp.ContractAddress,
	},
}

// subscriber implements Subscriber
type subscriber struct {
	nodeWSURL  string
	nodeRPCURL string
	chainID    ids.ID
	logsChan   chan vmtypes.WarpLogInfo
	evmLog     <-chan types.Log
	sub        interfaces.Subscription

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

	logs := make(chan vmtypes.WarpLogInfo, maxClientSubscriptionBuffer)

	return &subscriber{
		nodeWSURL:  subnetInfo.GetNodeWSEndpoint(),
		nodeRPCURL: subnetInfo.GetNodeRPCEndpoint(),
		chainID:    chainID,
		logger:     logger,
		db:         db,
		logsChan:   logs,
	}
}

func (s *subscriber) NewWarpLogInfo(log types.Log) (*vmtypes.WarpLogInfo, error) {
	if len(log.Topics) != 4 {
		s.logger.Error(
			"Log did not have the correct number of topics",
			zap.Int("numTopics", len(log.Topics)),
		)
		return nil, ErrInvalidLog
	}
	if log.Topics[0] != warp.WarpABI.Events["SendWarpMessage"].ID {
		s.logger.Error(
			"Log topic does not match the SendWarpMessage event type",
			zap.String("topic", log.Topics[0].String()),
			zap.String("expectedTopic", warp.WarpABI.Events["SendWarpMessage"].ID.String()),
		)
		return nil, ErrInvalidLog
	}
	destinationChainID, err := ids.ToID(log.Topics[1].Bytes())
	if err != nil {
		s.logger.Error(
			"Failed to decode destination chain ID",
			zap.Error(err),
		)
		return nil, ErrInvalidLog
	}

	return &vmtypes.WarpLogInfo{
		DestinationChainID: destinationChainID,
		DestinationAddress: log.Topics[2],
		SourceAddress:      log.Topics[3],
		SourceTxID:         log.TxHash[:],
		UnsignedMsgBytes:   log.Data,
		BlockNumber:        log.BlockNumber,
	}, nil
}

// forward logs from the concrete log channel to the interface channel
func (s *subscriber) forwardLogs() {
	for msgLog := range s.evmLog {
		messageInfo, err := s.NewWarpLogInfo(msgLog)
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

// Process logs from the given block height to the latest block
// Cap the number of blocks requested from the client to MaxBlocksToProcess,
// counting back from the current block.
func (s *subscriber) ProcessFromHeight(height *big.Int) error {
	s.logger.Info(
		"Processing historical logs",
		zap.String("fromBlockHeight", height.String()),
		zap.String("chainID", s.chainID.String()),
	)
	if height == nil {
		return fmt.Errorf("cannot process logs from nil height")
	}
	ethClient, err := ethclient.Dial(s.nodeRPCURL)
	if err != nil {
		return err
	}

	// Grab the latest block before filtering logs so we don't miss any before updating the db
	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		s.logger.Error(
			"Failed to get latest block",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}

	// Cap the number of blocks to process to MaxBlocksToProcess
	toBlock := big.NewInt(0).SetUint64(latestBlock)
	if height.Cmp(big.NewInt(0).Sub(toBlock, big.NewInt(MaxBlocksToProcess))) < 0 {
		s.logger.Warn(
			fmt.Sprintf("Requested to process too many blocks. Processing only the most recent %d blocks", MaxBlocksToProcess),
			zap.String("requestedBlockHeight", height.String()),
			zap.String("latestBlockHeight", toBlock.String()),
			zap.String("chainID", s.chainID.String()),
		)
		height = big.NewInt(0).Add(toBlock, big.NewInt(-MaxBlocksToProcess))
	}

	// Filter logs from the latest seen block to the latest block
	// Since initializationFilterQuery does not modify existing fields of warpFilterQuery,
	// we can safely reuse warpFilterQuery with only a shallow copy
	initializationFilterQuery := interfaces.FilterQuery{
		Topics:    warpFilterQuery.Topics,
		Addresses: warpFilterQuery.Addresses,
		FromBlock: height,
		ToBlock:   toBlock,
	}
	logs, err := ethClient.FilterLogs(context.Background(), initializationFilterQuery)
	if err != nil {
		s.logger.Error(
			"Failed to get logs on initialization",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}

	// Sort the logs in ascending block order. Order logs by index within blocks
	sort.SliceStable(logs, func(i, j int) bool {
		if logs[i].BlockNumber == logs[j].BlockNumber {
			return logs[i].TxIndex < logs[j].TxIndex
		}
		return logs[i].BlockNumber < logs[j].BlockNumber
	})

	// Queue each of the logs to be processed
	s.logger.Info(
		"Processing logs on initialization",
		zap.String("fromBlockHeight", height.String()),
		zap.String("toBlockHeight", toBlock.String()),
		zap.String("chainID", s.chainID.String()),
	)
	for _, log := range logs {
		messageInfo, err := s.NewWarpLogInfo(log)
		if err != nil {
			s.logger.Error(
				"Invalid log when processing from height. Continuing.",
				zap.String("chainID", s.chainID.String()),
				zap.Error(err),
			)
			continue
		}
		s.logsChan <- *messageInfo
	}

	return nil
}

func (s *subscriber) UpdateLatestSeenBlock() error {
	s.logger.Info(
		"Updating latest seen block in database",
		zap.String("chainID", s.chainID.String()),
	)
	ethClient, err := ethclient.Dial(s.nodeRPCURL)
	if err != nil {
		s.logger.Error(
			"Failed to dial node",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}

	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		s.logger.Error(
			"Failed to get latest block",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}

	err = s.db.Put(s.chainID, []byte(database.LatestSeenBlockKey), []byte(strconv.FormatUint(latestBlock, 10)))
	if err != nil {
		s.logger.Error(
			fmt.Sprintf("failed to put %s into database", database.LatestSeenBlockKey),
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}
	return nil
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

	evmLogs := make(chan types.Log, maxClientSubscriptionBuffer)
	sub, err := ethClient.SubscribeFilterLogs(context.Background(), warpFilterQuery, evmLogs)
	if err != nil {
		s.logger.Error(
			"Failed to subscribe to logs",
			zap.String("chainID", s.chainID.String()),
			zap.Error(err),
		)
		return err
	}
	s.evmLog = evmLogs
	s.sub = sub

	// Forward logs to the interface channel. Closed when the subscription is cancelled
	go s.forwardLogs()
	return nil
}

func (s *subscriber) Logs() <-chan vmtypes.WarpLogInfo {
	return s.logsChan
}

func (s *subscriber) Err() <-chan error {
	return s.sub.Err()
}

func (s *subscriber) Cancel() {
	// Nothing to do here, the ethclient manages both the log and err channels
}
