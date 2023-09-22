// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtypes

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

// WarpLogInfo is provided by the subscription and
// describes the transaction information emitted by the source chain,
// including the Warp Message payload bytes
type WarpLogInfo struct {
	SourceAddress      common.Hash
	DestinationChainID ids.ID
	DestinationAddress common.Hash
	SourceTxID         []byte
	UnsignedMsgBytes   []byte
	BlockNumber        uint64
}
