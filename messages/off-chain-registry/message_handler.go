// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/ethclient"
	teleporterregistry "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/upgrades/TeleporterRegistry"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

var OffChainRegistrySourceAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")

const (
	addProtocolVersionGasLimit  uint64 = 500_000
	revertVersionNotFoundString        = "TeleporterRegistry: version not found"
)

type factory struct {
	logger          logging.Logger
	registryAddress common.Address
}

type messageHandler struct {
	logger          logging.Logger
	unsignedMessage *warp.UnsignedMessage
	factory         *factory
}

func NewMessageHandlerFactory(
	logger logging.Logger,
	messageProtocolConfig config.MessageProtocolConfig,
) (messages.MessageHandlerFactory, error) {
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
	return &factory{
		logger:          logger,
		registryAddress: common.HexToAddress(messageConfig.TeleporterRegistryAddress),
	}, nil
}

func (f *factory) NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (messages.MessageHandler, error) {
	return &messageHandler{
		logger:          f.logger,
		unsignedMessage: unsignedMessage,
		factory:         f,
	}, nil
}

func (m *messageHandler) GetUnsignedMessage() *warp.UnsignedMessage {
	return m.unsignedMessage
}

// ShouldSendMessage returns false if any contract is already registered as the specified version in the TeleporterRegistry contract.
// This is because a single contract address can be registered to multiple versions, but each version may only map to a single contract address.
func (m *messageHandler) ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error) {
	addressedPayload, err := warpPayload.ParseAddressedCall(m.unsignedMessage.Payload)
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
	if destination != m.factory.registryAddress {
		m.logger.Info(
			"Message is not intended for the configured registry",
			zap.String("destination", destination.String()),
			zap.String("configuredRegistry", m.factory.registryAddress.String()),
		)
		return false, nil
	}

	// Get the correct destination client from the global map
	client, ok := destinationClient.Client().(ethclient.Client)
	if !ok {
		panic(fmt.Sprintf("Destination client for chain %s is not an Ethereum client", destinationClient.DestinationBlockchainID().String()))
	}

	// Check if the version is already registered in the TeleporterRegistry contract.
	registry, err := teleporterregistry.NewTeleporterRegistryCaller(m.factory.registryAddress, client)
	if err != nil {
		m.logger.Error(
			"Failed to create TeleporterRegistry caller",
			zap.Error(err),
		)
		return false, err
	}
	address, err := registry.GetAddressFromVersion(&bind.CallOpts{}, entry.Version)
	if err != nil {
		if strings.Contains(err.Error(), revertVersionNotFoundString) {
			return true, nil
		}
		m.logger.Error(
			"Failed to get address from version",
			zap.Error(err),
		)
		return false, err
	}

	m.logger.Info(
		"Version is already registered in the TeleporterRegistry contract",
		zap.String("version", entry.Version.String()),
		zap.String("registeredAddress", address.String()),
	)
	return false, nil
}

func (m *messageHandler) SendMessage(signedMessage *warp.Message, destinationClient vms.DestinationClient) error {
	// Construct the transaction call data to call the TeleporterRegistry contract.
	// Only one off-chain registry Warp message is sent at a time, so we hardcode the index to 0 in the call.
	callData, err := teleporterregistry.PackAddProtocolVersion(0)
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("destinationBlockchainID", destinationClient.DestinationBlockchainID().String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return err
	}

	_, err = destinationClient.SendTx(signedMessage, m.factory.registryAddress.Hex(), addProtocolVersionGasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("destinationBlockchainID", destinationClient.DestinationBlockchainID().String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationBlockchainID", destinationClient.DestinationBlockchainID().String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)
	return nil
}

func (m *messageHandler) GetMessageRoutingInfo() (
	ids.ID,
	common.Address,
	ids.ID,
	common.Address,
	error,
) {
	addressedPayload, err := warpPayload.ParseAddressedCall(m.unsignedMessage.Payload)
	if err != nil {
		m.logger.Error(
			"Failed parsing addressed payload",
			zap.Error(err),
		)
		return ids.ID{}, common.Address{}, ids.ID{}, common.Address{}, err
	}
	return m.unsignedMessage.SourceChainID,
		common.BytesToAddress(addressedPayload.SourceAddress),
		m.unsignedMessage.SourceChainID,
		m.factory.registryAddress,
		nil
}
