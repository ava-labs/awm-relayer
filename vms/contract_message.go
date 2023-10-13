// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/awm-relayer/vms/evm_block_hash"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
)

type ContractMessage interface {
	// UnpackWarpMessage unpacks the warp message from the VM
	UnpackWarpMessage(warpMessageInfo *vmtypes.WarpMessageInfo) error
}

func NewContractMessage(logger logging.Logger, subnetInfo config.SourceSubnet) ContractMessage {
	switch config.ParseVM(subnetInfo.VM) {
	case config.EVM:
		return evm.NewContractMessage(logger, subnetInfo)
	case config.EVM_BLOCKHASH:
		return evm_block_hash.NewContractMessage(logger, subnetInfo)
	default:
		return nil
	}
}
