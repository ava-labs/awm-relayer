// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ava-labs/avalanchego/ids"
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

type TeleporterMessageReceipt struct {
	ReceivedMessageID    *big.Int       `json:"receivedMessageID"`
	RelayerRewardAddress common.Address `json:"relayerRewardAddress"`
}

// ReceiveCrossChainMessageInput is the input to the ReceiveCrossChainMessage
// in the contract deployed on the receiving chain
type ReceiveCrossChainMessageInput struct {
	RelayerRewardAddress common.Address `json:"relayerRewardAddress"`
}

// MessageReceivedInput is the input to the MessageReceived
// in the contract deployed on the receiving chain
type MessageReceivedInput struct {
	OriginChainID ids.ID  `json:"relayerRewardAddress"`
	MessageID	 *big.Int `json:"messageID"`
}

// unpack Teleporter message bytes according to EVM ABI encoding rules
func unpackTeleporterMessage(messageBytes []byte) (*TeleporterMessage, error) {
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

func packReceiverMessage(inputStruct ReceiveCrossChainMessageInput) ([]byte, error) {
	return EVMTeleporterContractABI.Pack("receiveCrossChainMessage", inputStruct.RelayerRewardAddress)
}

func packMessageReceivedMessage(inputStruct MessageReceivedInput) ([]byte, error) {
	return EVMTeleporterContractABI.Pack("messageReceived", inputStruct.OriginChainID, inputStruct.MessageID)
}

func unpackMessageReceivedResult(result []byte) (bool, error) {
	var success bool
	err := EVMTeleporterContractABI.UnpackIntoInterface(&success, "messageReceived", result)
	return success, err
}