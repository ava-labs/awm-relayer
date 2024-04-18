// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
)

// WarpLogInfo is provided by the subscription and
// describes the transaction information emitted by the source chain,
// including the Warp Message payload bytes
type WarpLogInfo struct {
	SourceAddress    common.Address
	SourceTxID       []byte
	UnsignedMsgBytes []byte
	BlockNumber      uint64
}

type WarpBlockInfo struct {
	BlockNumber    uint64
	WarpLogs       []types.Log
	IsCatchUpBlock bool
}
