// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/hex"

	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ava-labs/subnet-evm/x/warp"
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
	m.logger.Info(
		"DBG: Warp log data",
		zap.String("unsignedMsgBytes", hex.EncodeToString(unsignedMsgBytes)),
	)
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(unsignedMsgBytes)
	if err != nil {
		m.logger.Warn(
			"Failed parsing unsigned message as log. Attempting to parse as standalone message",
			zap.Error(err),
		)
		unsignedMsg, err = avalancheWarp.ParseUnsignedMessage(unsignedMsgBytes)
		if err != nil {
			m.logger.Error(
				"Failed parsing unsigned message",
				zap.Error(err),
			)
			return nil, err
		}
	}
	m.logger.Info(
		"DBG: Parsed unsigned message",
		zap.String("unsignedMsg", hex.EncodeToString(unsignedMsg.Bytes())),
	)

	warpPayload, err := warpPayload.ParseAddressedCall(unsignedMsg.Payload)
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
