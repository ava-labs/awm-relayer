package chainlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/abi-bindings/eventimporter"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/messages/chainlink/proofs"
	"github.com/ava-labs/awm-relayer/relayer/config"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"
	subnetTypes "github.com/ava-labs/subnet-evm/core/types"
	subnetEthclient "github.com/ava-labs/subnet-evm/ethclient"
	subnetInterfaces "github.com/ava-labs/subnet-evm/interfaces"
	teleporterUtils "github.com/ava-labs/teleporter/utils/teleporter-utils"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"go.uber.org/zap"
)

type factory struct {
	logger logging.Logger
	config *Config
}

type ChainlinkMessageHandler struct {
	unsignedMessage         *warp.UnsignedMessage
	logger                  logging.Logger
	destinationBlockchainID ids.ID
	maxFilterAdresses       uint64
	aggregatorsToReplicas   map[common.Address]common.Address
}

type ChainlinkMessageDecoder struct {
	aggregators [][]common.Address
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

func NewMessageDecoder(messageProtocolConfig config.MessageProtocolConfig) (*ChainlinkMessageDecoder, error) {
	cfg, err := ParseConfig(messageProtocolConfig)
	if err != nil {
		return nil, err
	}
	if len(cfg.AggregatorsToReplicas) == 0 {
		return nil, errors.New("no aggregator to replica provided")
	}
	aggLen := uint64(len(cfg.AggregatorsToReplicas))
	chunksLen := (aggLen + cfg.MaxFilterAdresses - 1) / cfg.MaxFilterAdresses // ceil
	aggregators := make([][]common.Address, chunksLen)
	var chunkIndex uint64
	var chunkAggregators []common.Address
	for aggregator := range cfg.AggregatorsToReplicas {
		chunkAggregators = append(chunkAggregators, aggregator)
		if chunkIndex%cfg.MaxFilterAdresses == 0 || chunkIndex == aggLen-1 {
			aggregators = append(aggregators, chunkAggregators)
			chunkAggregators = make([]common.Address, cfg.MaxFilterAdresses)
		}
		chunkIndex++
	}
	if err != nil {
		return nil, err
	}
	return &ChainlinkMessageDecoder{
		aggregators: aggregators,
	}, nil
}

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
				logs := make([]subnetTypes.Log, 0)
				for _, aggregators := range c.aggregators {
					filteredLogs, err := ethClient.FilterLogs(context.Background(), subnetInterfaces.FilterQuery{
						Topics:    [][]common.Hash{{ChainlinkPriceUpdatedFilter}},
						Addresses: aggregators,
						FromBlock: header.Number,
						ToBlock:   header.Number,
					})
					if err != nil {
						return nil, err
					}
					logs = append(logs, filteredLogs...)
				}
				return logs, nil
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
	memDb, err := proofs.ConstructSubnetEVMReceiptProof(ctx, ethclient, log.BlockHash, log.TxIndex)
	if err != nil {
		return nil, err
	}
	it := memDb.NewIterator(nil, nil)
	receiptProof := make([][]byte, 0)
	for it.Next() {
		receiptProof = append(receiptProof, it.Value())
	}
	msg := ChainlinkMessage{
		aggregator:   log.Address,
		blockHeader:  blockHeader,
		txIndex:      new(big.Int).SetUint64(uint64(log.TxIndex)),
		receiptProof: receiptProof,
		logIndex:     new(big.Int).SetUint64(uint64(log.Index)),
		current:      log.Topics[1].Big(),
		roundId:      log.Topics[2].Big(),
		updatedAt:    log.Topics[3].Big(),
		data:         log.Data,
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

func ParseConfig(messageProtocolConfig config.MessageProtocolConfig) (*Config, error) {
	data, err := json.Marshal(messageProtocolConfig.Settings)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal Teleporter config: %w", err)
	}
	var messageConfig RawConfig
	if err := json.Unmarshal(data, &messageConfig); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal Teleporter config: %w", err)
	}

	config, err := messageConfig.Parse()
	if err != nil {
		return nil, err
	}

	return config, nil
}

func NewMessageHandlerFactory(
	logger logging.Logger,
	messageProtocolConfig config.MessageProtocolConfig,
) (messages.MessageHandlerFactory, error) {
	config, err := ParseConfig(messageProtocolConfig)
	if err != nil {
		return nil, err
	}

	return &factory{
		logger: logger,
		config: config,
	}, nil
}

func (f *factory) NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (messages.MessageHandler, error) {
	return &ChainlinkMessageHandler{
		logger:                  f.logger,
		unsignedMessage:         unsignedMessage,
		destinationBlockchainID: f.config.DestinationBlockchainID,
		maxFilterAdresses:       f.config.MaxFilterAdresses,
		aggregatorsToReplicas:   f.config.AggregatorsToReplicas,
	}, nil
}

func CalculateImportEventGasLimit(
	ctx context.Context,
	ethClient ethclient.Client,
	callData []byte,
	replica common.Address,
) (uint64, error) {
	gasPrice, err := ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return 0, err
	}

	return ethClient.EstimateGas(ctx, interfaces.CallMsg{
		From:     [20]byte{},
		To:       &replica,
		Gas:      0,
		GasPrice: gasPrice,
		Value:    big.NewInt(0),
		Data:     callData,
	})
}

func (c *ChainlinkMessageHandler) ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error) {
	return true, nil
}

func (c *ChainlinkMessageHandler) SendMessage(
	signedMessage *warp.Message,
	destinationClient vms.DestinationClient,
) (common.Hash, error) {
	var msg ChainlinkMessage
	if err := json.Unmarshal(signedMessage.Payload, &msg); err != nil {
		return common.Hash{}, err
	}
	destinationBlockchainID := destinationClient.DestinationBlockchainID()
	teleporterMessageID, err := teleporterUtils.CalculateMessageID(
		msg.aggregator,
		signedMessage.SourceChainID,
		destinationBlockchainID,
		msg.roundId,
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to calculate Teleporter message ID: %w", err)
	}

	c.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)

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
	subnetClient := destinationClient.Client().(subnetEthclient.Client)
	client := reflect.ValueOf(subnetClient).Elem().FieldByName("c").Interface().(ethclient.Client)
	gasLimit, err := CalculateImportEventGasLimit(context.TODO(), client, callData, replica)
	if err != nil {
		c.logger.Error(
			"Failed to calculate gas limit for importEvent call",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		return common.Hash{}, err
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

func (c *ChainlinkMessageHandler) GetMessageRoutingInfo(warpMessageInfo *relayerTypes.WarpMessageInfo) (
	ids.ID,
	common.Address,
	ids.ID,
	common.Address,
	error,
) {
	var msg ChainlinkMessage
	err := json.Unmarshal(warpMessageInfo.UnsignedMessage.Payload, &msg)
	if err != nil {
		return ids.Empty, common.Address{}, ids.Empty, common.Address{}, err
	}

	replica, ok := c.aggregatorsToReplicas[msg.aggregator]
	if !ok {
		err = fmt.Errorf("replica not found for aggregator %s", msg.aggregator)
		return ids.Empty, common.Address{}, ids.Empty, common.Address{}, err
	}

	return c.unsignedMessage.SourceChainID,
		warpMessageInfo.SourceAddress,
		c.destinationBlockchainID,
		replica,
		nil
}

func (c *ChainlinkMessageHandler) GetUnsignedMessage() *warp.UnsignedMessage {
	return c.unsignedMessage
}
