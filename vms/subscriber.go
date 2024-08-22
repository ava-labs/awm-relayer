// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/relayer/config"
	"github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
)

// Subscriber subscribes to VM events containing Warp message data. The events written to the
// channel returned by Logs() are assumed to be in block order. Logs within individual blocks
// may be in any order.
type Subscriber interface {
	// ProcessFromHeight processes events from {height} to the latest block.
	// Writes true to the channel on success, false on failure
	ProcessFromHeight(height *big.Int, done chan bool)

	// Subscribe registers a subscription. After Subscribe is called,
	// log events that match [filter] are written to the channel returned
	// by Logs
	Subscribe(maxResubscribeAttempts int) error

	// Headers returns the channel that the subscription writes block headers to
	Headers() <-chan *types.Header

	// Err returns the channel that the subscription writes errors to
	// If an error is sent to this channel, the subscription should be closed
	Err() <-chan error

	// Cancel cancels the subscription
	Cancel()
}

// NewSubscriber returns a concrete Subscriber according to the VM specified by [subnetInfo]
func NewSubscriber(
	logger logging.Logger,
	vm config.VM,
	blockchainID ids.ID,
	ethWSClient ethclient.Client,
	ethRPCClient ethclient.Client,
) Subscriber {
	switch vm {
	case config.EVM:
		return evm.NewSubscriber(logger, blockchainID, ethWSClient, ethRPCClient)
	default:
		return nil
	}
}
