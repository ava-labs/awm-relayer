// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// TeleporterMessage contains the Teleporter message, including
// the payload Message as a byte slice
type TeleporterMessage struct {
	MessageID               *big.Int                   `json:"messageID"`
	SenderAddress           common.Address             `json:"senderAddress"`
	DestinationAddress      common.Address             `json:"destinationAddress"`
	RequiredGasLimit        *big.Int                   `json:"requiredGasLimit"`
	AllowedRelayerAddresses []common.Address           `json:"allowedRelayerAddresses"`
	Receipts                []TeleporterMessageReceipt `json:"receipts"`
	Message                 []byte                     `json:"message"`
}

// TeleporterMessageReceipt corresponds to the receipt of a Teleporter message ID
// and the relayer reward address for that message
type TeleporterMessageReceipt struct {
	ReceivedMessageID    *big.Int       `json:"receivedMessageID"`
	RelayerRewardAddress common.Address `json:"relayerRewardAddress"`
}

// ReceiveCrossChainMessageInput is the input to receiveCrossChainMessage call
// in the contract deployed on the destination chain
type ReceiveCrossChainMessageInput struct {
	RelayerRewardAddress common.Address `json:"relayerRewardAddress"`
}

// MessageReceivedInput is the input to messageReceived call
// in the contract deployed on the destination chain
type MessageReceivedInput struct {
	OriginChainID ids.ID   `json:"relayerRewardAddress"`
	MessageID     *big.Int `json:"messageID"`
}

// UnpackTeleporterMessage unpacks message bytes according to EVM ABI encoding rules into a TeleporterMessage
func UnpackTeleporterMessage(messageBytes []byte) (*TeleporterMessage, error) {
	args := abi.Arguments{
		{
			Name: "teleporterMessage",
			Type: TeleporterMessageABI,
		},
	}
	unpacked, err := args.Unpack(messageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack to teleporter message with err: %v", err)
	}
	type teleporterMessageArg struct {
		TeleporterMessage TeleporterMessage `json:"teleporterMessage"`
	}
	var teleporterMessage teleporterMessageArg
	err = args.Copy(&teleporterMessage, unpacked)
	if err != nil {
		return nil, err
	}
	return &teleporterMessage.TeleporterMessage, nil
}

// PackReceiveCrossChainMessage packs a ReceiveCrossChainMessageInput to form a call to the receiveCrossChainMessage function
func PackReceiveCrossChainMessage(inputStruct ReceiveCrossChainMessageInput) ([]byte, error) {
	return EVMTeleporterContractABI.Pack("receiveCrossChainMessage", inputStruct.RelayerRewardAddress)
}

// PackMessageReceived packs a MessageReceivedInput to form a call to the messageReceived function
func PackMessageReceived(inputStruct MessageReceivedInput) ([]byte, error) {
	return EVMTeleporterContractABI.Pack("messageReceived", inputStruct.OriginChainID, inputStruct.MessageID)
}

// UnpackMessageReceivedResult attempts to unpack result bytes to a bool indicating whether the message was received
func UnpackMessageReceivedResult(result []byte) (bool, error) {
	var success bool
	err := EVMTeleporterContractABI.UnpackIntoInterface(&success, "messageReceived", result)
	return success, err
}

// PackSendCrossChainMessageEvent packs the SendCrossChainMessage event type. PackEvent is documented as not supporting struct types, so this should be used
// with caution. Here, we only use it for testing purposes. In a real setting, the Teleporter contract should pack the event.
func PackSendCrossChainMessageEvent(destinationChainID common.Hash, message TeleporterMessage) ([]byte, error) {
	_, hashes, err := EVMTeleporterContractABI.PackEvent("SendCrossChainMessage", destinationChainID, message.MessageID, message)
	return hashes, err
}
