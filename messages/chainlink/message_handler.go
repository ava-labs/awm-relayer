package chainlink

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/abi-bindings/eventimporter"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/relayer/config"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	subnetTypes "github.com/ava-labs/subnet-evm/core/types"
	subnetEthclient "github.com/ava-labs/subnet-evm/ethclient"
	subnetInterfaces "github.com/ava-labs/subnet-evm/interfaces"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"go.uber.org/zap"
)

type factory struct {
	logger logging.Logger
	config *Config
}

type ChainlinkMessageHandler struct {
	unsignedMessage       *warp.UnsignedMessage
	logger                logging.Logger
	maxFilterAdresses     uint64
	aggregatorsToReplicas map[common.Address]common.Address
	aggregators           []common.Address
}

type ChainlinkMessageDecoder struct {
	handler *ChainlinkMessageHandler
}

type ChainlinkMessage struct {
	aggregator common.Address

	blockHeader  []byte
	txIndex      *big.Int
	receiptProof [][]byte
	logIndex     *big.Int

	current   *big.Int
	roundId   *big.Int
	updatedAt *big.Int
	data      []byte
}

var ChainlinkPriceUpdatedFilter = common.HexToHash("0559884fd3a460db3073b7fc896cc77986f16e378210ded43186175bf646fc5f")

func (c ChainlinkMessageDecoder) Decode(
	ctx context.Context,
	header *subnetTypes.Header,
	ethClient subnetEthclient.Client,
) ([]*relayerTypes.WarpMessageInfo, error) {
	var (
		logs []subnetTypes.Log
		err  error
	)
	// Check if the block contains warp logs, and fetch them from the client if it does
	if header.Bloom.Test(ChainlinkPriceUpdatedFilter[:]) {
		cctx, cancel := context.WithTimeout(context.Background(), utils.DefaultRPCRetryTimeout)
		defer cancel()
		logs, err = utils.CallWithRetry[[]subnetTypes.Log](
			cctx,
			func() ([]subnetTypes.Log, error) {
				return ethClient.FilterLogs(context.Background(), subnetInterfaces.FilterQuery{
					Topics:    [][]common.Hash{{ChainlinkPriceUpdatedFilter}},
					Addresses: c.handler.aggregators,
					FromBlock: header.Number,
					ToBlock:   header.Number,
				})
			})
		if err != nil {
			return nil, err
		}
	}
	messages := make([]*relayerTypes.WarpMessageInfo, len(logs))
	for i, log := range logs {
		warpLog, err := NewWarpMessageInfo(ctx, log, ethClient)
		if err != nil {
			return nil, err
		}
		messages[i] = warpLog
	}

	return messages, nil
}

func NewWarpMessageInfo(
	ctx context.Context,
	log subnetTypes.Log,
	ethclient subnetEthclient.Client,
) (
	*relayerTypes.WarpMessageInfo,
	error,
) {
	if len(log.Topics) != 4 {
		return nil, relayerTypes.ErrInvalidLog
	}
	if log.Topics[0] != ChainlinkPriceUpdatedFilter {
		return nil, relayerTypes.ErrInvalidLog
	}
	block, err := ethclient.BlockByHash(ctx, log.BlockHash)
	if err != nil {
		return nil, err
	}
	blockHeader, err := rlp.EncodeToBytes(block.Header)
	if err != nil {
		return nil, err
	}
	msg := ChainlinkMessage{
		aggregator:  log.Address,
		blockHeader: blockHeader,
		current:     log.Topics[1].Big(),
		roundId:     log.Topics[2].Big(),
		updatedAt:   log.Topics[3].Big(),
		data:        log.Data,
	}
	unsignedMsg, err := ConvertToUnsignedMessage(&msg)
	if err != nil {
		return nil, err
	}

	return &relayerTypes.WarpMessageInfo{
		SourceAddress:   common.BytesToAddress(log.Address[:]),
		UnsignedMessage: unsignedMsg,
	}, nil
}

func ConvertToUnsignedMessage(msg *ChainlinkMessage) (*warp.UnsignedMessage, error) {
	bytes, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return warp.ParseUnsignedMessage(bytes)
}

func NewMessageHandlerFactory(
	logger logging.Logger,
	messageProtocolConfig config.MessageProtocolConfig,
) (messages.MessageHandlerFactory, error) {
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		logger.Error("Failed to marshal Teleporter config")
		return nil, err
	}
	var messageConfig RawConfig
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		logger.Error("Failed to unmarshal Teleporter config")
		return nil, err
	}

	config, err := messageConfig.Parse()
	if err != nil {
		return nil, err
	}

	return &factory{
		logger: logger,
		config: config,
	}, nil
}

func (f *factory) NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (messages.MessageHandler, error) {
	aggregatorsToReplicas := f.config.AggregatorsToReplicas
	aggregators := make([]common.Address, len(aggregatorsToReplicas))
	for aggregator := range aggregatorsToReplicas {
		aggregators = append(aggregators, aggregator)
	}

	return &ChainlinkMessageHandler{
		logger:                f.logger,
		unsignedMessage:       unsignedMessage,
		maxFilterAdresses:     f.config.MaxFilterAdresses,
		aggregatorsToReplicas: aggregatorsToReplicas,
		aggregators:           aggregators,
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
	var msg ChainlinkMessage
	if err := json.Unmarshal(signedMessage.Payload, &msg); err != nil {
		return common.Hash{}, err
	}
	callData, err := eventimporter.PackImportEvent(msg.blockHeader, msg.txIndex, msg.receiptProof, msg.logIndex)
	if err != nil {
		c.logger.Error(
			"Failed packing importEvent call data",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return common.Hash{}, err
	}

	replica, ok := c.aggregatorsToReplicas[msg.aggregator]
	if !ok {
		c.logger.Error(
			"Failed to find replica for aggregator",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.Error(err),
		)
		return common.Hash{}, fmt.Errorf("failed to find replica for aggregator: %s", msg.aggregator)
	}
	txHash, err := destinationClient.SendTx(
		signedMessage,
		replica.Hex(),
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
