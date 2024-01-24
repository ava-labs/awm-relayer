package offchainregistry

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
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

func (m *messageManager) ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationBlockchainID ids.ID) (bool, error) {
	return true, nil

}

func (m *messageManager) SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationBlockchainID ids.ID) error {
	return nil
}

func (m *messageManager) GetDestinationBlockchainID(warpMessageInfo *vmtypes.WarpMessageInfo) (ids.ID, error) {
	return warpMessageInfo.WarpUnsignedMessage.SourceChainID, nil
}
