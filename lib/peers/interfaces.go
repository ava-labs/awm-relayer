// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
)

type AppRequestNetwork interface {
	ConnectPeers(set.Set[ids.NodeID]) set.Set[ids.NodeID]
	ConnectToCanonicalValidators(subnetID ids.ID) (*ConnectedCanonicalValidators, error)
	Send(message.OutboundMessage, set.Set[ids.NodeID], ids.ID, subnets.Allower) set.Set[ids.NodeID]

	// Handler methods
	RegisterAppRequest(ids.RequestID)
	RegisterRequestID(uint32, int) chan message.InboundMessage

	GetSubnetID(blockchainID ids.ID) (ids.ID, error)
}
