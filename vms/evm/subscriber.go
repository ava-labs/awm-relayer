// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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
	log        chan vmtypes.WarpLogInfo
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
		log:        logs,
	}
}

func NewWarpLogInfo(log types.Log) (*vmtypes.WarpLogInfo, error) {
	if len(log.Topics) != 4 {
		return nil, ErrInvalidLog
	}
	if log.Topics[0] != warp.WarpABI.Events["SendWarpMessage"].ID {
		return nil, ErrInvalidLog
	}
	destinationChainID, err := ids.ToID(log.Topics[1].Bytes())
	if err != nil {
		return nil, ErrInvalidLog
	}

	return &vmtypes.WarpLogInfo{
		DestinationChainID: destinationChainID,
		DestinationAddress: log.Topics[2],
		SourceAddress:      log.Topics[3],
		SourceTxID:         log.TxHash[:],
		UnsignedMsgBytes:   log.Data,
	}, nil
}

// forward logs from the concrete log channel to the interface channel
func (s *subscriber) forwardLogs() {
	for msgLog := range s.evmLog {
		messageInfo, err := NewWarpLogInfo(msgLog)
		if err != nil {
			s.logger.Info(
				"Invalid log. Continuing.",
				zap.Error(err),
			)
			continue
		}
		s.log <- *messageInfo

		// Update the database with the latest block height
		// TODO: This should also be done in a separate goroutine, rather than waiting for warp messages to be processed
		// TODO: Rather than updating the db when logs are received, we may want to consider updating the db when messages are successfully relayed
		err = s.db.Put(s.chainID, []byte(database.LatestBlockHeightKey), []byte(strconv.FormatUint(msgLog.BlockNumber, 10)))
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to put %s into database", database.LatestBlockHeightKey), zap.Error(err))
		}
	}
}

func (s *subscriber) Initialize() error {
	ethClient, err := ethclient.Dial(s.nodeRPCURL)
	if err != nil {
		return err
	}

	// Get the latest processed block height from the database.
	heightData, err := s.db.Get(s.chainID, []byte(database.LatestBlockHeightKey))
	if err != nil {
		s.logger.Warn("failed to get latest block from database", zap.Error(err))
		return err
	}
	latestBlockHeight, success := new(big.Int).SetString(string(heightData), 10)
	if !success {
		s.logger.Error("failed to convert latest block to big.Int", zap.Error(err))
		return err
	}

	// Grab the latest block before filtering logs so we don't miss any before updating the db
	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		s.logger.Error(
			"Failed to get latest block",
			zap.Error(err),
		)
		return err
	}

	// Filter logs from the latest processed block to the latest block
	// Since initializationFilterQuery does not modify existing fields of warpFilterQuery,
	// we can safely reuse warpFilterQuery with only a shallow copy
	initializationFilterQuery := interfaces.FilterQuery{
		Topics:    warpFilterQuery.Topics,
		Addresses: warpFilterQuery.Addresses,
		FromBlock: latestBlockHeight,
	}
	logs, err := ethClient.FilterLogs(context.Background(), initializationFilterQuery)
	if err != nil {
		s.logger.Error(
			"Failed to get logs on initialization",
			zap.Error(err),
		)
		return err
	}

	// Queue each of the logs to be processed
	s.logger.Info(
		"Processing logs on initialization",
		zap.String("fromBlockHeight", latestBlockHeight.String()),
		zap.String("toBlockHeight", strconv.Itoa(int(latestBlock))),
	)
	for _, log := range logs {
		messageInfo, err := NewWarpLogInfo(log)
		if err != nil {
			s.logger.Info(
				"Invalid log on initialization. Continuing.",
				zap.Error(err),
			)
			continue
		}
		s.log <- *messageInfo
	}

	// Update the database with the latest block height
	err = s.db.Put(s.chainID, []byte(database.LatestBlockHeightKey), []byte(strconv.FormatUint(latestBlock, 10)))
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to put %s into database", database.LatestBlockHeightKey), zap.Error(err))
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
			s.logger.Info("Successfully subscribed")
			return nil
		}

		s.logger.Warn(
			"Failed to subscribe to node",
			zap.Int("attempt", attempt),
			zap.Error(err),
		)

		if attempt != maxResubscribeAttempts-1 {
			time.Sleep(subscribeRetryTimeout)
		}
	}

	return fmt.Errorf("failed to subscribe to node with all %d attempts", maxResubscribeAttempts)
}

func (s *subscriber) dialAndSubscribe() error {
	// Dial the configured destination chain endpoint
	// This needs to be a websocket
	ethClient, err := ethclient.Dial(s.nodeWSURL)
	if err != nil {
		return err
	}

	filterQuery := warpFilterQuery
	evmLogs := make(chan types.Log, maxClientSubscriptionBuffer)
	sub, err := ethClient.SubscribeFilterLogs(context.Background(), filterQuery, evmLogs)
	if err != nil {
		s.logger.Error(
			"Failed to subscribe to logs",
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
	return s.log
}

func (s *subscriber) Err() <-chan error {
	return s.sub.Err()
}

func (s *subscriber) Cancel() {
	// Nothing to do here, the ethclient manages both the log and err channels
}
