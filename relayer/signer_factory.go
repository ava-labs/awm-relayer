package relayer

import (
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
)

type MessageSignerFactory func(requestID uint32) MessageSigner

type MessageSigner interface {
	SignMessage(unsignedMessage *avalancheWarp.UnsignedMessage) (*avalancheWarp.Message, error)
}

func NewAppRequestNetworkSignerFactory(
	logger logging.Logger,
	network AppRequestNetwork,
	srcBlockchain config.SourceBlockchain,
	destBlockchain config.DestinationBlockchain,
	messageCreator message.Creator,
) MessageSignerFactory {
	signingSubnet := srcBlockchain.GetSubnetID()
	if signingSubnet == constants.PrimaryNetworkID {
		signingSubnet = destBlockchain.GetSubnetID()
	}
	quorum := destBlockchain.GetWarpQuorum()
	messageSignerFactory := func(requestID uint32) MessageSigner {
		return NewAppRequestMessageSigner(
			logger,
			network,
			srcBlockchain.GetBlockchainID(),
			srcBlockchain.GetSubnetID(),
			destBlockchain.GetBlockchainID(),
			messageCreator,
			signingSubnet,
			quorum,
			requestID,
		)
	}
	return messageSignerFactory
}

func NewRPCSignerFactory(
	logger logging.Logger,
	srcBlockchain config.SourceBlockchain,
	destBlockchain config.DestinationBlockchain,
) MessageSignerFactory {
	return func(requestID uint32) MessageSigner {
		return NewRPCMessageSigner(srcBlockchain, destBlockchain, warpBackend.NewClient, logger)
	}
}
