// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"
	"errors"
	"fmt"
	"time"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ethereum/go-ethereum/common"
)

var WarpPrecompileLogFilter = warp.WarpABI.Events["SendWarpMessage"].ID
var ErrInvalidLog = errors.New("invalid warp message log")

const (
	filterLogsRetries = 5
	retryInterval     = 1 * time.Second
)

// WarpBlockInfo describes the block height and logs needed to process Warp messages.
// WarpBlockInfo instances are populated by the subscriber, and forwared to the
// Listener to process
type WarpBlockInfo struct {
	BlockNumber uint64
	Messages    []*WarpMessageInfo
}

// WarpMessageInfo describes the transaction information for the Warp message
// sent on the source chain.
// WarpMessageInfo instances are either derived from the logs of a block or
// from the manual Warp message information provided via configuration.
type WarpMessageInfo struct {
	SourceAddress   common.Address
	UnsignedMessage *avalancheWarp.UnsignedMessage
}

// Extract Warp logs from the block, if they exist
func NewWarpBlockInfo(header *types.Header, ethClient ethclient.Client) (*WarpBlockInfo, error) {
	var (
		logs []types.Log
		err  error
	)
	// Check if the block contains warp logs, and fetch them from the client if it does
	if header.Bloom.Test(WarpPrecompileLogFilter[:]) {
		logs, err = fetchWarpLogsWithRetries(ethClient, header, filterLogsRetries, retryInterval)
		if err != nil {
			return nil, err
		}
	}
	messages := make([]*WarpMessageInfo, len(logs))
	for i, log := range logs {
		warpLog, err := NewWarpMessageInfo(log)
		if err != nil {
			return nil, err
		}
		messages[i] = warpLog
	}

	return &WarpBlockInfo{
		BlockNumber: header.Number.Uint64(),
		Messages:    messages,
	}, nil
}

// Extract the Warp message information from the raw log
func NewWarpMessageInfo(log types.Log) (*WarpMessageInfo, error) {
	if len(log.Topics) != 3 {
		return nil, ErrInvalidLog
	}
	if log.Topics[0] != WarpPrecompileLogFilter {
		return nil, ErrInvalidLog
	}
	unsignedMsg, err := UnpackWarpMessage(log.Data)
	if err != nil {
		return nil, err
	}

	return &WarpMessageInfo{
		SourceAddress:   common.BytesToAddress(log.Topics[1][:]),
		UnsignedMessage: unsignedMsg,
	}, nil
}

func UnpackWarpMessage(unsignedMsgBytes []byte) (*avalancheWarp.UnsignedMessage, error) {
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(unsignedMsgBytes)
	if err != nil {
		// If we failed to parse the message as a log, attempt to parse it as a standalone message
		var standaloneErr error
		unsignedMsg, standaloneErr = avalancheWarp.ParseUnsignedMessage(unsignedMsgBytes)
		if standaloneErr != nil {
			err = errors.Join(err, standaloneErr)
			return nil, err
		}
	}
	return unsignedMsg, nil
}

// The node serving the filter logs request may be behind the node serving the block header request,
// so we retry a few times to ensure we get the logs
func fetchWarpLogsWithRetries(ethClient ethclient.Client, header *types.Header, numRetries int, retryInterval time.Duration) ([]types.Log, error) {
	var (
		logs []types.Log
		err  error
	)

	for i := 0; i < numRetries; i++ {
		logs, err = ethClient.FilterLogs(context.Background(), interfaces.FilterQuery{
			Topics:    [][]common.Hash{{WarpPrecompileLogFilter}},
			Addresses: []common.Address{warp.ContractAddress},
			FromBlock: header.Number,
			ToBlock:   header.Number,
		})
		if err == nil {
			return logs, nil
		}
		if i != numRetries-1 {
			time.Sleep(retryInterval)
		}
	}
	return nil, fmt.Errorf("failed to fetch warp logs for block %d after %d retries: %w", header.Number.Uint64(), filterLogsRetries, err)
}
