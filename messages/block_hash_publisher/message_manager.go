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
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	publishBlockHashGasLimit = 100000 // TODONOW: set the correct gas limit
)

type destinationSenderInfo struct {
	client       vms.DestinationClient
	address      common.Address
	lastTimeSent time.Time
	lastBlock    uint64
}

type messageManager struct {
	messageConfig Config
	destinations  map[ids.ID]destinationSenderInfo
	logger        logging.Logger
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*messageManager, error) {
	// Marshal the map and unmarshal into the Teleporter config
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

	destinations := make(map[ids.ID]destinationSenderInfo)
	for _, destination := range messageConfig.DestinationChains {
		destinationID, err := ids.FromString(destination.ChainID)
		if err != nil {
			logger.Error(
				"Failed to decode base-58 encoded destination chain ID",
				zap.Error(err),
			)
			return nil, err
		}
		destinations[destinationID] = destinationSenderInfo{
			address: common.HexToAddress(destination.Address),
			client:  destinationClients[destinationID],
		}
	}

	return &messageManager{
		messageConfig: messageConfig,
		destinations:  destinations,
		logger:        logger,
	}, nil
}

// ShouldSendMessage returns true if the message should be sent to the destination chain
func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationChainID ids.ID) (bool, error) {
	// TODONOW: check if the message should be sent to the destination chain based on the configured block/time interval
	destination, ok := m.destinations[destinationChainID]
	if !ok {
		return false, fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID)
	}
	// TODO: config should be a map of destinationChainID -> config
	// TODO: need to get the height+timestamp of the block with the hash
	if m.messageConfig.DestinationChains[destinationChainID].useTimeInterval {
		interval := m.messageConfig.DestinationChains[destinationChainID].timeIntervalSeconds
		if time.Since(destination.lastTimeSent) < interval {
			return false, nil
		}
	} else {
		interval := m.messageConfig.DestinationChains[destinationChainID].blockInterval
		if warpMessageInfo.BlockNumber-destination.lastBlock < uint64(interval) {
			return false, nil
		}
	}
	return true, nil
}

func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationChainID ids.ID) error {
	// TODONOW: Set the calldata by packing the ABI arguments
	var callData []byte

	// Get the correct destination client from the global map
	destination, ok := m.destinations[destinationChainID]
	if !ok {
		return fmt.Errorf("relayer not configured to deliver to destination. destinationChainID=%s", destinationChainID)
	}
	err := destination.client.SendTx(signedMessage, destination.address.Hex(), publishBlockHashGasLimit, callData)
	if err != nil {
		m.logger.Error(
			"Failed to send tx.",
			zap.String("destinationChainID", destinationChainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.Error(err),
		)
		return err
	}
	// TODONOW: set the time/block number of the last sent message
	m.logger.Info(
		"Sent message to destination chain",
		zap.String("destinationChainID", destinationChainID.String()),
		zap.String("warpMessageID", signedMessage.ID().String()),
	)
	return nil
}
