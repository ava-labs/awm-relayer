package offchainregistry

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	teleporterregistry "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/upgrades/TeleporterRegistry"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var (
	OffChainRegistrySourceAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")
)

const (
	addProtocolVersionGasLimit uint64 = 500_000
)

type messageManager struct {
	logger             logging.Logger
	destinationClients map[ids.ID]vms.DestinationClient
	registryAddress    common.Address
}

func NewMessageManager(
	logger logging.Logger,
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
	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. DestinationBlockchainID=%s", destinationBlockchainID)
	}

	// Construct the transaction call data to call the TeleporterRegistry contract.
	callData, err := teleporterregistry.PackAddProtocolVersion(0)
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}

	err = destinationClient.SendTx(signedMessage, m.registryAddress.Hex(), addProtocolVersionGasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)
	return nil
}

func (m *messageManager) GetDestinationBlockchainID(unsignedMessage *warp.UnsignedMessage) (ids.ID, error) {
	return unsignedMessage.SourceChainID, nil
}
