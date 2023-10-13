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
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	"github.com/ethereum/go-ethereum/common"
)

// MessageManager is specific to each message protocol. The interface handles choosing which messages to send
// for each message protocol, and performs the sending to the destination chain.
type MessageManager interface {
	// ShouldSendMessage returns true if the message should be sent to the destination chain
	// If an error is returned, the boolean should be ignored by the caller.
	ShouldSendMessage(warpMessageInfo *vmtypes.WarpMessageInfo, destinationChainID ids.ID) (bool, error)
	// SendMessage sends the signed message to the destination chain. The payload parsed according to
	// the VM rules is also passed in, since MessageManager does not assume any particular VM
	SendMessage(signedMessage *warp.Message, parsedVmPayload []byte, destinationChainID ids.ID) error
}

// NewMessageManager constructs a MessageManager for a particular message protocol, defined by the message protocol address and config
// Note that DestinationClients may be invoked concurrently by many MessageManagers, so it is assumed that they are implemented in a thread-safe way
func NewMessageManager(
	logger logging.Logger,
	messageProtocolAddress common.Hash,
	messageProtocolConfig config.MessageProtocolConfig,
	destinationClients map[ids.ID]vms.DestinationClient,
	allowedDestinationChainIDs map[ids.ID]bool,
) (MessageManager, error) {
	format := messageProtocolConfig.MessageFormat
	switch config.ParseMessageProtocol(format) {
	case config.TELEPORTER:
		return teleporter.NewMessageManager(logger,
			messageProtocolAddress,
			messageProtocolConfig,
			destinationClients,
			allowedDestinationChainIDs,
		)
	default:
		return nil, fmt.Errorf("invalid message format %s", format)
	}
}
