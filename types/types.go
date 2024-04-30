// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
)

// WarpBlockInfo describes the block height and logs needed to process Warp messages.
// WarpBlockInfo instances are populated by the subscriber, and forwared to the
// listener to process
type WarpBlockInfo struct {
	BlockNumber uint64
	WarpLogs    []types.Log
}

// WarpLogInfo describes the transaction information for the Warp message
// sent on the source chain, and includes the Warp Message payload bytes
// WarpLogInfo instances are either derived from the logs of a block or
// from the manual Warp message information provided via configuration
type WarpLogInfo struct {
	SourceAddress    common.Address
	UnsignedMsgBytes []byte
}
