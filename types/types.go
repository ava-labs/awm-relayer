// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"
	"errors"
	"math/big"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ethereum/go-ethereum/common"
)

var WarpPrecompileLogFilter = warp.WarpABI.Events["SendWarpMessage"].ID
var ErrInvalidLog = errors.New("invalid warp message log")

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
	MessageManager  messages.MessageManager
}

// Extract Warp logs from the block, if they exist
func NewWarpBlockInfo(header *types.Header, ethClient ethclient.Client) (*WarpBlockInfo, error) {
	var (
		logs []types.Log
		err  error
	)
	// Check if the block contains warp logs, and fetch them from the client if it does
	if header.Bloom.Test(WarpPrecompileLogFilter[:]) {
		logs, err = ethClient.FilterLogs(context.Background(), interfaces.FilterQuery{
			Topics:    [][]common.Hash{{WarpPrecompileLogFilter}},
			Addresses: []common.Address{warp.ContractAddress},
			FromBlock: big.NewInt(int64(header.Number.Uint64())),
			ToBlock:   big.NewInt(int64(header.Number.Uint64())),
		})
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
	unsignedMsgBytes, err := UnpackWarpMessage(log.Data)
	if err != nil {
		return nil, err
	}

	return &WarpMessageInfo{
		SourceAddress:   common.BytesToAddress(log.Topics[1][:]),
		UnsignedMessage: unsignedMsgBytes,
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
