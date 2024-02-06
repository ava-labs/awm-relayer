package offchainregistry

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/ethclient"
	teleporterregistry "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/upgrades/TeleporterRegistry"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var (
	OffChainRegistrySourceAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")
)

const (
	addProtocolVersionGasLimit  uint64 = 500_000
	revertVersionNotFoundString        = "TeleporterRegistry: version not found"
	revertAddressNotFoundString        = "TeleporterRegistry: protocol address not found"
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

// ShouldSendMessage returns false if the version/protocol address tuple is already registered in the TeleporterRegistry contract.
func (m *messageManager) ShouldSendMessage(unsignedMessage *warp.UnsignedMessage, destinationBlockchainID ids.ID) (bool, error) {
	addressedPayload, err := warpPayload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		m.logger.Error(
			"Failed parsing addressed payload",
			zap.Error(err),
		)
		return false, err
	}
	entry, destination, err := teleporterregistry.UnpackTeleporterRegistryWarpPayload(addressedPayload.Payload)
	if err != nil {
		m.logger.Error(
			"Failed unpacking teleporter registry warp payload",
			zap.Error(err),
		)
		return false, err
	}
	if destination != m.registryAddress {
		m.logger.Info(
			"Message is not intended for the configured registry",
			zap.String("destination", destination.String()),
			zap.String("configuredRegistry", m.registryAddress.String()),
		)
		return false, nil
	}

	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return false, fmt.Errorf("relayer not configured to deliver to destination. destinationBlockchainID=%s", destinationBlockchainID.String())
	}
	client, ok := destinationClient.Client().(ethclient.Client)
	if !ok {
		panic(fmt.Sprintf("Destination client for chain %s is not an Ethereum client", destinationClient.DestinationBlockchainID().String()))
	}

	// Check if the version/protocol address tuple is already registered in the TeleporterRegistry contract.
	registry, err := teleporterregistry.NewTeleporterRegistryCaller(m.registryAddress, client)
	if err != nil {
		m.logger.Error(
			"Failed to create TeleporterRegistry caller",
			zap.Error(err),
		)
		return false, err
	}
	address, err := registry.GetAddressFromVersion(&bind.CallOpts{}, entry.Version)
	if err != nil {
		// Check if execution reverted due to the version not being found
		if strings.Contains(err.Error(), revertVersionNotFoundString) {
			return true, nil
		}
		m.logger.Error(
			"Failed to get address from version",
			zap.Error(err),
		)
		return false, err
	}
	if address == entry.ProtocolAddress {
		m.logger.Info(
			"Version/protocol address tuple is already registered in the TeleporterRegistry contract",
			zap.String("version", entry.Version.String()),
			zap.String("protocolAddress", entry.ProtocolAddress.String()),
		)
		return false, nil
	}

	version, err := registry.GetVersionFromAddress(&bind.CallOpts{}, entry.ProtocolAddress)
	if err != nil {
		// Check if execution reverted due to the address not being found
		if strings.Contains(err.Error(), revertAddressNotFoundString) {
			return true, nil
		}
		m.logger.Error(
			"Failed to get version from address",
			zap.Error(err),
		)
		return false, err
	}
	if version.Cmp(entry.Version) == 0 {
		m.logger.Info(
			"Version/protocol address tuple is already registered in the TeleporterRegistry contract",
			zap.String("version", entry.Version.String()),
			zap.String("protocolAddress", entry.ProtocolAddress.String()),
		)
		return false, nil
	}

	return true, nil
}

func (m *messageManager) SendMessage(signedMessage *warp.Message, destinationBlockchainID ids.ID) error {
	// Get the correct destination client from the global map
	destinationClient, ok := m.destinationClients[destinationBlockchainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. DestinationBlockchainID=%s", destinationBlockchainID)
	}

	// Construct the transaction call data to call the TeleporterRegistry contract.
	// Only one off-chain registry Warp message is sent at a time, so we hardcode the index to 0 in the call.
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
