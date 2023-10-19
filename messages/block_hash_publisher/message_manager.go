package block_hash_publisher

import (
	"encoding/json"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	teleporter_block_hash "github.com/ava-labs/teleporter/abi-bindings/Teleporter/TeleporterBlockHashReceiver"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	publishBlockHashGasLimit = 275000
)

type destinationSenderInfo struct {
	client  vms.DestinationClient
	address common.Address

	useTimeInterval     bool
	timeIntervalSeconds uint64
	blockInterval       uint64

	lastTimeSent uint64
	lastBlock    uint64
}

func (d *destinationSenderInfo) shouldSend(blockTimestamp uint64, blockNumber uint64) bool {
	if d.useTimeInterval {
		interval := d.timeIntervalSeconds
		if time.Unix(int64(blockTimestamp), 0).Sub(time.Unix(int64(d.lastTimeSent), 0)) < (time.Duration(interval) * time.Second) {
			return false
		}
	} else {
		interval := d.blockInterval
		if blockNumber-d.lastBlock < interval {
			return false
		}
	}
	return true
}

type messageManager struct {
	destinations map[ids.ID]*destinationSenderInfo
	logger       logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*messageManager, error) {
	// Marshal the map and unmarshal into the Block Hash Publisher config
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

	destinations := make(map[ids.ID]*destinationSenderInfo)
	for _, destination := range messageConfig.DestinationChains {
		destinationID, err := ids.FromString(destination.ChainID)
		if err != nil {
			logger.Error(
				"Failed to decode base-58 encoded destination chain ID",
				zap.Error(err),
			)
			return nil, err
		}
		destinations[destinationID] = &destinationSenderInfo{
			useTimeInterval:     destination.useTimeInterval,
			timeIntervalSeconds: destination.timeIntervalSeconds,
			blockInterval:       destination.blockInterval,
			address:             common.HexToAddress(destination.Address),
			client:              destinationClients[destinationID],
		}
	}

	return &messageManager{
		destinations: destinations,
		logger:       logger,
	}, nil
}

// ShouldSendMessage returns true if the message should be sent to ANY of the configured destination chains
// This saves us from having to aggregate signatures in that case. Decisions about which destination chains to send to are made in SendMessage
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, _ ids.ID) (bool, error) {
	// TODO: Handle the primary network case. If it's the primary network, only check the passed in destinationChainID
	for _, destination := range m.destinations {
		if destination.shouldSend(warpMessageInfo.BlockTimestamp, warpMessageInfo.BlockNumber) {
			return true, nil
		}
	}
	return false, nil
}

func (m *messageManager) SendMessage(signedMessage *warp.Message, warpMessageInfo *vmtypes.WarpMessageInfo, _ ids.ID) error {
	// TODO: Handle the primary network case. If it's the primary network, only send to the passed in destinationChainID
	for destinationChainID, destination := range m.destinations {
		if !destination.shouldSend(warpMessageInfo.BlockTimestamp, warpMessageInfo.BlockNumber) {
			m.logger.Debug(
				"Not sending message to destination chain",
				zap.String("destinationChainID", destinationChainID.String()),
				zap.String("warpMessageID", signedMessage.ID().String()),
			)
			continue
		}

		m.logger.Info(
			"Sending message to destination chain",
			zap.String("destinationChainID", destinationChainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
		// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
		callData, err := teleporter_block_hash.PackReceiveBlockHash(0)
		if err != nil {
			m.logger.Error(
				"Failed packing receiveCrossChainMessage call data",
				zap.Error(err),
				zap.Uint32("messageIndex", 0),
				zap.String("sourceChainID", signedMessage.SourceChainID.String()),
				zap.String("destinationChainID", destinationChainID.String()),
				zap.String("warpMessageID", signedMessage.ID().String()),
			)
			return err
		}

		// Get the correct destination client from the global map
		err = destination.client.SendTx(signedMessage, destination.address.Hex(), publishBlockHashGasLimit, callData)
		if err != nil {
			m.logger.Error(
				"Failed to send tx.",
				zap.String("destinationChainID", destinationChainID.String()),
				zap.String("warpMessageID", signedMessage.ID().String()),
				zap.Error(err),
			)
			return err
		}

		// Set the last sent block/time
		destination.lastTimeSent = warpMessageInfo.BlockTimestamp
		destination.lastBlock = warpMessageInfo.BlockNumber
		m.logger.Info(
			"Sent message to destination chain",
			zap.String("destinationChainID", destinationChainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
		)
	}
	return nil
}
