package evm_block_hash

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

func (m *contractMessage) UnpackWarpMessage(warpMessageInfo *vmtypes.WarpMessageInfo) error {
	unsignedMsg, err := avalancheWarp.ParseUnsignedMessage(warpMessageInfo.UnsignedMsgBytes)
	if err != nil {
		m.logger.Error(
			"Failed parsing unsigned message",
			zap.Error(err),
		)
		return err
	}
	err = unsignedMsg.Initialize()
	if err != nil {
		m.logger.Error(
			"Failed initializing unsigned message",
			zap.Error(err),
		)
		return err
	}

	warpPayload, err := warpPayload.ParseBlockHashPayload(unsignedMsg.Payload)
	if err != nil {
		m.logger.Error(
			"Failed parsing addressed payload",
			zap.Error(err),
		)
		return err
	}

	warpMessageInfo.WarpUnsignedMessage = unsignedMsg
	warpMessageInfo.WarpPayload = warpPayload.BlockHash.Bytes()

	return nil
}
