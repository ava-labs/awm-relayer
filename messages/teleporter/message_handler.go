// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/messages"
	pbDecider "github.com/ava-labs/awm-relayer/proto/pb/decider"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/teleporter/TeleporterMessenger"
	gasUtils "github.com/ava-labs/teleporter/utils/gas-utils"
	teleporterUtils "github.com/ava-labs/teleporter/utils/teleporter-utils"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type factory struct {
	messageConfig   Config
	protocolAddress common.Address
	logger          logging.Logger
	deciderClient   pbDecider.DeciderServiceClient
}

type messageHandler struct {
	logger            logging.Logger
	teleporterMessage *teleportermessenger.TeleporterMessage
	unsignedMessage   *warp.UnsignedMessage
	factory           *factory
	deciderClient     pbDecider.DeciderServiceClient
}

func NewMessageHandlerFactory(
	logger logging.Logger,
	messageProtocolAddress common.Address,
	messageProtocolConfig config.MessageProtocolConfig,
	deciderClientConn *grpc.ClientConn,
) (messages.MessageHandlerFactory, error) {
	// Marshal the map and unmarshal into the Teleporter config
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		logger.Error("Failed to marshal Teleporter config")
		return nil, err
	}
	var messageConfig Config
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		logger.Error("Failed to unmarshal Teleporter config")
		return nil, err
	}

	if err := messageConfig.Validate(); err != nil {
		logger.Error(
			"Invalid Teleporter config.",
			zap.Error(err),
		)
		return nil, err
	}

	var deciderClient pbDecider.DeciderServiceClient
	if deciderClientConn != nil {
		deciderClient = pbDecider.NewDeciderServiceClient(deciderClientConn)
	}

	return &factory{
		messageConfig:   messageConfig,
		protocolAddress: messageProtocolAddress,
		logger:          logger,
		deciderClient:   deciderClient,
	}, nil
}

func (f *factory) NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (messages.MessageHandler, error) {
	teleporterMessage, err := f.parseTeleporterMessage(unsignedMessage)
	if err != nil {
		f.logger.Error(
			"Failed to parse teleporter message.",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		return nil, err
	}
	return &messageHandler{
		logger:            f.logger,
		teleporterMessage: teleporterMessage,
		unsignedMessage:   unsignedMessage,
		factory:           f,
		deciderClient:     f.deciderClient,
	}, nil
}

func isAllowedRelayer(allowedRelayers []common.Address, eoa common.Address) bool {
	// If no allowed relayer addresses were set, then anyone can relay it.
	if len(allowedRelayers) == 0 {
		return true
	}

	for _, addr := range allowedRelayers {
		if addr == eoa {
			return true
		}
	}
	return false
}

func (m *messageHandler) GetUnsignedMessage() *warp.UnsignedMessage {
	return m.unsignedMessage
}

func (m *messageHandler) GetMessageRoutingInfo() (
	ids.ID,
	common.Address,
	ids.ID,
	common.Address,
	error,
) {
	return m.unsignedMessage.SourceChainID,
		m.teleporterMessage.OriginSenderAddress,
		m.teleporterMessage.DestinationBlockchainID,
		m.teleporterMessage.DestinationAddress,
		nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageHandler) ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error) {
	destinationBlockchainID := destinationClient.DestinationBlockchainID()
	teleporterMessageID, err := teleporterUtils.CalculateMessageID(
		m.factory.protocolAddress,
		m.unsignedMessage.SourceChainID,
		destinationBlockchainID,
		m.teleporterMessage.MessageNonce,
	)
	if err != nil {
		return false, fmt.Errorf("failed to calculate Teleporter message ID: %w", err)
	}

	senderAddress := destinationClient.SenderAddress()
	if !isAllowedRelayer(m.teleporterMessage.AllowedRelayerAddresses, senderAddress) {
		m.logger.Info(
			"Relayer EOA not allowed to deliver this message.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", m.unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return false, nil
	}

	// Check if the message has already been delivered to the destination chain
	teleporterMessenger := m.factory.getTeleporterMessenger(destinationClient)
	delivered, err := teleporterMessenger.MessageReceived(&bind.CallOpts{}, teleporterMessageID)
	if err != nil {
		m.logger.Error(
			"Failed to check if message has been delivered to destination chain.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", m.unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return false, err
	}
	if delivered {
		m.logger.Info(
			"Message already delivered to destination.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return false, nil
	}
	// Dispatch to the external decider service. If the service is unavailable or returns
	// an error, then use the decision that has already been made, i.e. return true
	decision, err := m.getShouldSendMessageFromDecider()
	if err != nil {
		m.logger.Warn(
			"Error delegating to decider",
			zap.String("warpMessageID", m.unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return true, nil
	}
	if !decision {
		m.logger.Info(
			"Decider rejected message",
			zap.String("warpMessageID", m.unsignedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		)
	}
	return decision, nil
}

// Queries the decider service to determine whether this message should be
// sent. If the decider client is nil, returns true.
func (m *messageHandler) getShouldSendMessageFromDecider() (bool, error) {
	deciderClientValue := reflect.ValueOf(m.deciderClient)

	if !deciderClientValue.IsValid() || deciderClientValue.IsNil() {
		return true, nil
	}

	warpMsgID := m.unsignedMessage.ID()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCtx()
	response, err := m.deciderClient.ShouldSendMessage(
		ctx,
		&pbDecider.ShouldSendMessageRequest{
			NetworkId:           m.unsignedMessage.NetworkID,
			SourceChainId:       m.unsignedMessage.SourceChainID[:],
			Payload:             m.unsignedMessage.Payload,
			BytesRepresentation: m.unsignedMessage.Bytes(),
			Id:                  warpMsgID[:],
		},
	)
	if err != nil {
		m.logger.Error("Error response from decider.", zap.Error(err))
		return false, err
	}

	return response.ShouldSendMessage, nil
}

// SendMessage extracts the gasLimit and packs the call data to call the receiveCrossChainMessage
// method of the Teleporter contract, and dispatches transaction construction and broadcast to the
// destination client.
func (m *messageHandler) SendMessage(
	signedMessage *warp.Message,
	destinationClient vms.DestinationClient,
) (common.Hash, error) {
	destinationBlockchainID := destinationClient.DestinationBlockchainID()
	teleporterMessageID, err := teleporterUtils.CalculateMessageID(
		m.factory.protocolAddress,
		signedMessage.SourceChainID,
		destinationBlockchainID,
		m.teleporterMessage.MessageNonce,
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to calculate Teleporter message ID: %w", err)
	}

	m.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessageID.String()),
	)
	numSigners, err := signedMessage.Signature.NumSigners()
	if err != nil {
		m.logger.Error(
			"Failed to get number of signers",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return common.Hash{}, err
	}

	gasLimit, err := gasUtils.CalculateReceiveMessageGasLimit(
		numSigners,
		m.teleporterMessage.RequiredGasLimit,
		len(signedMessage.Bytes()),
		len(signedMessage.Payload),
		len(m.teleporterMessage.Receipts),
	)
	if err != nil {
		m.logger.Error(
			"Failed to calculate gas limit for receiveCrossChainMessage call",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return common.Hash{}, err
	}
	// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
	callData, err := teleportermessenger.PackReceiveCrossChainMessage(
		0,
		common.HexToAddress(m.factory.messageConfig.RewardAddress),
	)
	if err != nil {
		m.logger.Error(
			"Failed packing receiveCrossChainMessage call data",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
		)
		return common.Hash{}, err
	}

	txHash, err := destinationClient.SendTx(
		signedMessage,
		m.factory.protocolAddress.Hex(),
		gasLimit,
		callData,
	)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return common.Hash{}, err
	}

	// Wait for the message to be included in a block before returning
	err = m.waitForReceipt(signedMessage, destinationClient, txHash, teleporterMessageID)
	if err != nil {
		return common.Hash{}, err
	}

	m.logger.Info(
		"Delivered message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("teleporterMessageID", teleporterMessageID.String()),
		zap.String("txHash", txHash.String()),
	)
	return txHash, nil
}

func (m *messageHandler) waitForReceipt(
	signedMessage *warp.Message,
	destinationClient vms.DestinationClient,
	txHash common.Hash,
	teleporterMessageID ids.ID,
) error {
	destinationBlockchainID := destinationClient.DestinationBlockchainID()
	callCtx, callCtxCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer callCtxCancel()
	receipt, err := utils.CallWithRetry[*types.Receipt](
		callCtx,
		func() (*types.Receipt, error) {
			return destinationClient.Client().(ethclient.Client).TransactionReceipt(callCtx, txHash)
		},
	)
	if err != nil {
		m.logger.Error(
			"Failed to get transaction receipt",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		m.logger.Error(
			"Transaction failed",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.String("txHash", txHash.String()),
		)
		return fmt.Errorf("transaction failed with status: %d", receipt.Status)
	}
	return nil
}

// parseTeleporterMessage returns the Warp message's corresponding Teleporter message from the cache if it exists.
// Otherwise parses the Warp message payload.
func (f *factory) parseTeleporterMessage(
	unsignedMessage *warp.UnsignedMessage,
) (*teleportermessenger.TeleporterMessage, error) {
	addressedPayload, err := warpPayload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		f.logger.Error(
			"Failed parsing addressed payload",
			zap.Error(err),
		)
		return nil, err
	}
	teleporterMessage, err := teleportermessenger.UnpackTeleporterMessage(addressedPayload.Payload)
	if err != nil {
		f.logger.Error(
			"Failed unpacking teleporter message.",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
		)
		return nil, err
	}

	return teleporterMessage, nil
}

// getTeleporterMessenger returns the Teleporter messenger instance for the destination chain.
// Panic instead of returning errors because this should never happen, and if it does, we do not
// want to log and swallow the error, since operations after this will fail too.
func (f *factory) getTeleporterMessenger(
	destinationClient vms.DestinationClient,
) *teleportermessenger.TeleporterMessenger {
	client, ok := destinationClient.Client().(ethclient.Client)
	if !ok {
		panic(fmt.Sprintf(
			"Destination client for chain %s is not an Ethereum client",
			destinationClient.DestinationBlockchainID().String()),
		)
	}

	// Get the teleporter messenger contract
	teleporterMessenger, err := teleportermessenger.NewTeleporterMessenger(f.protocolAddress, client)
	if err != nil {
		panic(fmt.Sprintf("Failed to get teleporter messenger contract: %s", err.Error()))
	}
	return teleporterMessenger
}
