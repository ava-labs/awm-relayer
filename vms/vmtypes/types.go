// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtypes

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

//
// Types used in the vms interfaces
//

// WarpMessageInfo is used internally to provide access to warp message info emitted by the sender
type WarpMessageInfo struct {
	WarpUnsignedMessage *warp.UnsignedMessage
	WarpPayload         []byte
}
