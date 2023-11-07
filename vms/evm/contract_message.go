// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"go.uber.org/zap"
)

type contractMessage struct {
	logger logging.Logger
}

func NewContractMessage(logger logging.Logger, subnetInfo config.SourceSubnet) *contractMessage {
	return &contractMessage{
		logger: logger,
	}
}

func (m *contractMessage) UnpackWarpMessage(unsignedMsgBytes []byte) (*vmtypes.WarpMessageInfo, error) {
	unsignedMsg, err := avalancheWarp.ParseUnsignedMessage(unsignedMsgBytes)
	if err != nil {
		m.logger.Error(
			"Failed parsing unsigned message",
			zap.Error(err),
		)
		return nil, err
	}

	warpPayload, err := warpPayload.ParseAddressedPayload(unsignedMsg.Payload)
	if err != nil {
		m.logger.Error(
			"Failed parsing addressed payload",
			zap.Error(err),
		)
		return nil, err
	}

	messageInfo := vmtypes.WarpMessageInfo{
		WarpUnsignedMessage: unsignedMsg,
		WarpPayload:         warpPayload.Payload,
	}
	return &messageInfo, nil
}
