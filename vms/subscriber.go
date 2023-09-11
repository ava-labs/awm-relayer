// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"math/big"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
)

// Subscriber subscribes to VM events containing Warp message data. The events written to the
// channel returned by Logs() are assumed to be in block order. Logs within individual blocks
// may be in any order.
type Subscriber interface {
	// ProcessFromHeight processes events from {height} to the latest block
	ProcessFromHeight(height *big.Int) error

	// UpdateLatestSeenBlock retrieves the latest block from the chain and updates the database
	UpdateLatestSeenBlock() error

	// Subscribe registers a subscription. After Subscribe is called,
	// log events that match [filter] are written to the channel returned
	// by Logs
	Subscribe() error

	// Logs returns the channel that the subscription writes events to
	Logs() <-chan vmtypes.WarpLogInfo

	// Err returns the channel that the subscription writes errors to
	// If an error is sent to this channel, the subscription should be closed
	Err() <-chan error

	// Cancel cancels the subscription
	Cancel()
}

// NewSubscriber returns a concrete Subscriber according to the VM specified by [subnetInfo]
func NewSubscriber(logger logging.Logger, subnetInfo config.SourceSubnet, db database.RelayerDatabase) Subscriber {
	switch config.ParseVM(subnetInfo.VM) {
	case config.EVM:
		return evm.NewSubscriber(logger, subnetInfo, db)
	default:
		return nil
	}
}
