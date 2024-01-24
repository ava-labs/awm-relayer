package offchainregistry

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type messageManager struct {
	logger             logging.Logger
	destinationClients map[ids.ID]vms.DestinationClient
	registryAddress    common.Address
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Address,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*messageManager, error) {
	// Marshal the map and unmarshal into the off-chain registry config
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		logger.Error("Failed to marshal off-chain registry config")
		return nil, err
	}
	var messageConfig Config
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		logger.Error("Failed to unmarshal off-chain registry config")
		return nil, err
	}

	if err := messageConfig.Validate(); err != nil {
		logger.Error(
			"Invalid off-chain registry config.",
			zap.Error(err),
		)
		return nil, err
	}
	return &messageManager{
		logger:             logger,
		destinationClients: destinationClients,
		registryAddress:    common.HexToAddress(messageConfig.TeleporterRegistryAddress),
	}, nil
}

func (m *messageManager) ShouldSendMessage(unsignedMessage *warp.UnsignedMessage, destinationBlockchainID ids.ID) (bool, error) {
	return true, nil

}

func (m *messageManager) SendMessage(signedMessage *warp.Message, destinationBlockchainID ids.ID) error {
	//TODONOW: Implement
	return nil
}

func (m *messageManager) GetDestinationBlockchainID(unsignedMessage *warp.UnsignedMessage) (ids.ID, error) {
	return unsignedMessage.SourceChainID, nil
}
