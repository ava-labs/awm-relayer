// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
)

// Subscriber subscribes to VM events containing Warp message data
type Subscriber interface {
	// Subscribe registers a subscription. After Subscribe is called,
	// matching log events should be written to the channel returned
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
func NewSubscriber(logger logging.Logger, subnetInfo config.SourceSubnet) Subscriber {
	switch config.ParseVM(subnetInfo.VM) {
	case config.EVM:
		return evm.NewSubscriber(logger, subnetInfo)
	default:
		return nil
	}
}
