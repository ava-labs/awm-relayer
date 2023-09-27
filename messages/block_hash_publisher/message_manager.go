package block_hash_publisher

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type messageManager struct {
	messageConfig      Config
	destinationClients map[ids.ID]vms.DestinationClient
	logger             logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*messageManager, error) {
	// Marshal the map and unmarshal into the Teleporter config
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		logger.Error("Failed to marshal Block Hash Publisher config")
		return nil, err
	}
	var messageConfig Config
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		logger.Error("Failed to unmarshal Block Hash Publisher config")
		return nil, err
	}

	if err := messageConfig.Validate(); err != nil {
		logger.Error(
			"Invalid Block Hash Publisher config.",
			zap.Error(err),
		)
		return nil, err
	}

	return &messageManager{
		messageConfig:      messageConfig,
		destinationClients: destinationClients,
		logger:             logger,
	}, nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationChainID ids.ID) (bool, error) {
	// TODONOW: check if the message should be sent to the destination chain based on the configured block/time interval
	return true, nil
}

func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationChainID ids.ID) error {
	// TODONOW: send the message to the destination chain
	return nil
}
