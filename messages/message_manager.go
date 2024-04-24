// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_message_manager.go -package=mocks

package messages

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ethereum/go-ethereum/common"
)

// MessageManager is specific to each message protocol. The interface handles choosing which messages to send
// for each message protocol, and performs the sending to the destination chain.
type MessageManager interface {
	// ShouldSendMessage returns true if the message should be sent to the destination chain
	// If an error is returned, the boolean should be ignored by the caller.
	ShouldSendMessage(unsignedMessage *warp.UnsignedMessage, destinationBlockchainID ids.ID) (bool, error)

	// SendMessage sends the signed message to the destination chain. The payload parsed according to
	// the VM rules is also passed in, since MessageManager does not assume any particular VM
	SendMessage(signedMessage *warp.Message, destinationBlockchainID ids.ID) error

	// GetMessageRoutingInfo returns the source chain ID, origin sender address, destination chain ID, and destination address
	GetMessageRoutingInfo(unsignedMessage *warp.UnsignedMessage) (
		ids.ID,
		common.Address,
		ids.ID,
		common.Address,
		error,
	)
}

// NewMessageManager constructs a MessageManager for a particular message protocol, defined by the message protocol address and config
// Note that DestinationClients may be invoked concurrently by many MessageManagers, so it is assumed that they are implemented in a thread-safe way
func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Address,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
) (MessageManager, error) {
	format := messageProtocolConfig.MessageFormat
	switch config.ParseMessageProtocol(format) {
	case config.TELEPORTER:
		return teleporter.NewMessageManager(
			logger,
			messageProtocolAddress,
			messageProtocolConfig,
			destinationClients,
		)
	case config.OFF_CHAIN_REGISTRY:
		return offchainregistry.NewMessageManager(
			logger,
			messageProtocolConfig,
			destinationClients,
		)
	default:
		return nil, fmt.Errorf("invalid message format %s", format)
	}
}
