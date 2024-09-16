package chainlink

import (
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/abi-bindings/eventimporter"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/relayer/config"
	"github.com/ava-labs/awm-relayer/vms"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type factory struct {
	logger     logging.Logger
	aggregator common.Address
}

type ChainlinkMessageHandler struct {
	unsignedMessage *warp.UnsignedMessage
	logger          logging.Logger
	aggregator      common.Address
}

func NewMessageHandlerFactory(
	logger logging.Logger,
	messageProtocolConfig config.MessageProtocolConfig,
) (messages.MessageHandlerFactory, error) {
	return &factory{
		logger:     logger,
		aggregator: common.Address{},
	}, nil
}

func (f *factory) NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (messages.MessageHandler, error) {
	return &ChainlinkMessageHandler{
		logger:          f.logger,
		unsignedMessage: unsignedMessage,
		aggregator:      f.aggregator,
	}, nil
}

func CalculateImportEventGasLimit() (uint64, error) {
	return 0, nil
}

func (c *ChainlinkMessageHandler) ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error) {
	return true, nil
}

func (c *ChainlinkMessageHandler) SendMessage(
	signedMessage *warp.Message,
	destinationClient vms.DestinationClient,
) (common.Hash, error) {
	destinationBlockchainID := destinationClient.DestinationBlockchainID()

	c.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)

	gasLimit, err := CalculateImportEventGasLimit()
	if err != nil {
		c.logger.Error(
			"Failed to calculate gas limit for receiveCrossChainMessage call",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return common.Hash{}, err
	}
	blockHeader := []byte{}
	txIndex := big.NewInt(0)
	receiptProof := [][]byte{}
	logIndex := big.NewInt(0)
	callData, err := eventimporter.PackImportEvent(blockHeader, txIndex, receiptProof, logIndex)
	if err != nil {
		c.logger.Error(
			"Failed packing importEvent call data",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return common.Hash{}, err
	}

	txHash, err := destinationClient.SendTx(
		signedMessage,
		c.aggregator.Hex(),
		gasLimit,
		callData,
	)
	if err != nil {
		c.logger.Error(
			"Failed to send tx.",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.Error(err),
		)
		return common.Hash{}, err
	}

	teleporterMessageID := ids.Empty
	// Wait for the message to be included in a block before returning
	err = messages.WaitForReceipt(c.logger, signedMessage, destinationClient, txHash, teleporterMessageID)
	if err != nil {
		return common.Hash{}, err
	}

	c.logger.Info(
		"Delivered message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
		zap.String("txHash", txHash.String()),
	)
	return txHash, nil
}

func (c *ChainlinkMessageHandler) GetMessageRoutingInfo() (
	ids.ID,
	common.Address,
	ids.ID,
	common.Address,
	error,
) {
	return ids.Empty, common.Address{}, ids.Empty, common.Address{}, nil
}

func (c *ChainlinkMessageHandler) GetUnsignedMessage() *warp.UnsignedMessage {
	return c.unsignedMessage
}
