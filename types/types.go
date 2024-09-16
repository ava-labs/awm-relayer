// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"errors"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ethereum/go-ethereum/common"
)

var (
	WarpPrecompileLogFilter = warp.WarpABI.Events["SendWarpMessage"].ID
	ErrInvalidLog           = errors.New("invalid warp message log")
)

// WarpBlockInfo describes the block height and logs needed to process Warp messages.
// WarpBlockInfo instances are populated by the subscriber, and forwarded to the Listener to process.
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
