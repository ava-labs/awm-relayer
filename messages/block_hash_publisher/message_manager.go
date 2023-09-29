package block_hash_publisher

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	teleporter_block_hash "github.com/ava-labs/teleporter/abis/go/teleporter-block-hash"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	publishBlockHashGasLimit = 1000000 // TODONOW: set the correct gas limit
)

type destinationSenderInfo struct {
	client  vms.DestinationClient
	address common.Address

	useTimeInterval     bool
	timeIntervalSeconds uint64
	blockInterval       uint64

	lastApprovedTime  uint64
	lastTimeSent      uint64
	lastApprovedBlock uint64
	lastBlock         uint64
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
	logger.Info("DEBUG CONFIG", zap.String("config", fmt.Sprintf("%#v", messageConfig)))

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
			timeIntervalSeconds: uint64(destination.timeIntervalSeconds),
			blockInterval:       uint64(destination.blockInterval),
			address:             common.HexToAddress(destination.Address),
			client:              destinationClients[destinationID],
		}
		logger.Info("DEBUG DESTINATIONS", zap.String("address", destinations[destinationID].address.String()))

	}

	return &messageManager{
		destinations: destinations,
		logger:       logger,
	}, nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationChainID ids.ID) (bool, error) {
	destination, ok := m.destinations[destinationChainID]
	if !ok {
		var destinationIDs []string
		for id := range m.destinations {
			destinationIDs = append(destinationIDs, id.String())
		}
		m.logger.Info(
			"DEBUG",
			zap.String("destinationChainID", destinationChainID.String()),
			zap.String("configuredDestinations", fmt.Sprintf("%#v", destinationIDs)),
		)
		return false, fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID)
	}
	if destination.useTimeInterval {
		interval := destination.timeIntervalSeconds
		if time.Unix(int64(warpMessageInfo.BlockTimestamp), 0).Sub(time.Unix(int64(destination.lastTimeSent), 0)) < (time.Duration(interval) * time.Second) {
			return false, nil
		}
	} else {
		interval := destination.blockInterval
		if warpMessageInfo.BlockNumber-destination.lastBlock < uint64(interval) {
			m.logger.Info(
				"DEBUG",
				zap.String("decision", "Not sending"),
				zap.Int("blockNum", int(warpMessageInfo.BlockNumber)),
				zap.Int("lastBlockNum", int(destination.lastBlock)),
			)
			return false, nil
		}
	}
	// Set the last approved block/time here. We don't set the last sent block/time until the message is actually sent
	destination.lastApprovedBlock = warpMessageInfo.BlockNumber
	destination.lastApprovedTime = warpMessageInfo.BlockTimestamp
	m.logger.Info(
		"DEBUG",
		zap.String("decision", "Sending"),
		zap.Int("blockNum", int(warpMessageInfo.BlockNumber)),
		zap.Int("lastBlockNum", int(destination.lastBlock)),
	)
	return true, nil
}

func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationChainID ids.ID) error {
	m.logger.Info(
		"Sending message to destination chain",
		zap.String("destinationChainID", destinationChainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)
	// Construct the transaction call data to call the receive cross chain message method of the receiver precompile.
	callData, err := teleporter_block_hash.PackReceiveBlockHash(teleporter_block_hash.ReceiveBlockHashInput{
		MessageIndex:  uint32(0),
		SourceChainID: signedMessage.SourceChainID,
	})
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
	destination, ok := m.destinations[destinationChainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID)
	}
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
	destination.lastTimeSent = destination.lastApprovedTime
	destination.lastBlock = destination.lastApprovedBlock
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationChainID", destinationChainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)
	return nil
}
