package offchainregistry

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ethereum/go-ethereum/common"
)

type messageManager struct {
	logger             logging.Logger
	destinationClients map[ids.ID]vms.DestinationClient
}

func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Address,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (*messageManager, error) {
	return &messageManager{
		logger:             logger,
		destinationClients: destinationClients,
	}, nil
}

func (m *messageManager) ShouldSendMessage(unsignedMessage *warp.UnsignedMessage, destinationBlockchainID ids.ID) (bool, error) {
	return true, nil

}

func (m *messageManager) SendMessage(signedMessage *warp.Message, destinationAddress common.Address, destinationBlockchainID ids.ID) error {
	return nil
}

func (m *messageManager) GetDestinationBlockchainID(unsignedMessage *warp.UnsignedMessage) (ids.ID, error) {
	return unsignedMessage.SourceChainID, nil
}
