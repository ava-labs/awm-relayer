// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_message_handler.go -package=mocks

package messages

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ethereum/go-ethereum/common"
)

// MessageManager is specific to each message protocol. The interface handles choosing which messages to send
// for each message protocol, and performs the sending to the destination chain.
type MessageHandlerFactory interface {
	// Create a message handler to relay the Warp message
	NewMessageHandler(unsignedMessage *warp.UnsignedMessage) (MessageHandler, error)
}

// MessageHandlers relay a single Warp message. A new instance should be created for each Warp message.
type MessageHandler interface {
	// ShouldSendMessage returns true if the message should be sent to the destination chain
	// If an error is returned, the boolean should be ignored by the caller.
	ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error)

	// SendMessage sends the signed message to the destination chain. The payload parsed according to
	// the VM rules is also passed in, since MessageManager does not assume any particular VM
	SendMessage(signedMessage *warp.Message, destinationClient vms.DestinationClient) error

	// GetMessageRoutingInfo returns the source chain ID, origin sender address, destination chain ID, and destination address
	GetMessageRoutingInfo() (
		ids.ID,
		common.Address,
		ids.ID,
		common.Address,
		error,
	)

	// GetUnsignedMessage returns the unsigned message
	GetUnsignedMessage() *warp.UnsignedMessage
}
