// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtypes

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ethereum/go-ethereum/common"
)

// WarpMessageInfo is provided by the subscription and
// describes the transaction information emitted by the source chain,
// including the Warp Message payload bytes
type WarpMessageInfo struct {
	SourceAddress       common.Address
	SourceTxID          []byte
	UnsignedMsgBytes    []byte
	BlockNumber         uint64
	IsCatchUpMessage    bool
	WarpUnsignedMessage *warp.UnsignedMessage
	WarpPayload         []byte
}
