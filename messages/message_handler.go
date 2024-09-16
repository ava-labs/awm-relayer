// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_message_handler.go -package=mocks

package messages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	subnetTypes "github.com/ava-labs/subnet-evm/core/types"
	subnetEthclient "github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// MessageManager is specific to each message protocol. The interface handles choosing which messages to send
// for each message protocol, and performs the sending to the destination chain.
type MessageHandlerFactory interface {
	// Create a message handler to relay the Warp message
	NewMessageHandler(unsignedMessage *avalancheWarp.UnsignedMessage) (MessageHandler, error)
}

// MessageHandlers relay a single Warp message. A new instance should be created for each Warp message.
type MessageHandler interface {
	// ShouldSendMessage returns true if the message should be sent to the destination chain
	// If an error is returned, the boolean should be ignored by the caller.
	ShouldSendMessage(destinationClient vms.DestinationClient) (bool, error)

	// SendMessage sends the signed message to the destination chain. The payload parsed according to
	// the VM rules is also passed in, since MessageManager does not assume any particular VM
	// returns the transaction hash if the transaction is successful.
	SendMessage(signedMessage *avalancheWarp.Message, destinationClient vms.DestinationClient) (common.Hash, error)

	// GetMessageRoutingInfo returns the source chain ID, origin sender address,
	// destination chain ID, and destination address.
	GetMessageRoutingInfo() (
		ids.ID,
		common.Address,
		ids.ID,
		common.Address,
		error,
	)

	// GetUnsignedMessage returns the unsigned message
	GetUnsignedMessage() *avalancheWarp.UnsignedMessage
}

type MessageDecoder interface {
	Decode(header *subnetTypes.Header, ethClient subnetEthclient.Client) ([]*relayerTypes.WarpMessageInfo, error)
}

type WarpMessageDecoder struct{}

// Extract Warp logs from the block, if they exist
func (w WarpMessageDecoder) Decode(
	header *subnetTypes.Header,
	ethClient subnetEthclient.Client,
) ([]*relayerTypes.WarpMessageInfo, error) {
	var (
		logs []subnetTypes.Log
		err  error
	)
	// Check if the block contains warp logs, and fetch them from the client if it does
	if header.Bloom.Test(relayerTypes.WarpPrecompileLogFilter[:]) {
		cctx, cancel := context.WithTimeout(context.Background(), utils.DefaultRPCRetryTimeout)
		defer cancel()
		logs, err = utils.CallWithRetry[[]subnetTypes.Log](
			cctx,
			func() ([]subnetTypes.Log, error) {
				return ethClient.FilterLogs(context.Background(), interfaces.FilterQuery{
					Topics:    [][]common.Hash{{relayerTypes.WarpPrecompileLogFilter}},
					Addresses: []common.Address{warp.ContractAddress},
					FromBlock: header.Number,
					ToBlock:   header.Number,
				})
			})
		if err != nil {
			return nil, err
		}
	}
	messages := make([]*relayerTypes.WarpMessageInfo, len(logs))
	for i, log := range logs {
		warpLog, err := NewWarpMessageInfo(log)
		if err != nil {
			return nil, err
		}
		messages[i] = warpLog
	}

	return messages, nil
}

// Extract the Warp message information from the raw log
func NewWarpMessageInfo(log subnetTypes.Log) (*relayerTypes.WarpMessageInfo, error) {
	if len(log.Topics) != 3 {
		return nil, relayerTypes.ErrInvalidLog
	}
	if log.Topics[0] != relayerTypes.WarpPrecompileLogFilter {
		return nil, relayerTypes.ErrInvalidLog
	}
	unsignedMsg, err := UnpackWarpMessage(log.Data)
	if err != nil {
		return nil, err
	}

	return &relayerTypes.WarpMessageInfo{
		SourceAddress:   common.BytesToAddress(log.Topics[1][:]),
		UnsignedMessage: unsignedMsg,
	}, nil
}

func UnpackWarpMessage(unsignedMsgBytes []byte) (*avalancheWarp.UnsignedMessage, error) {
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(unsignedMsgBytes)
	if err != nil {
		// If we failed to parse the message as a log, attempt to parse it as a standalone message
		var standaloneErr error
		unsignedMsg, standaloneErr = avalancheWarp.ParseUnsignedMessage(unsignedMsgBytes)
		if standaloneErr != nil {
			err = errors.Join(err, standaloneErr)
			return nil, err
		}
	}
	return unsignedMsg, nil
}

func WaitForReceipt(
	logger logging.Logger,
	signedMessage *avalancheWarp.Message,
	destinationClient vms.DestinationClient,
	txHash common.Hash,
	teleporterMessageID ids.ID,
) error {
	destinationBlockchainID := destinationClient.DestinationBlockchainID()
	callCtx, callCtxCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer callCtxCancel()
	receipt, err := utils.CallWithRetry[*types.Receipt](
		callCtx,
		func() (*types.Receipt, error) {
			return destinationClient.Client().(ethclient.Client).TransactionReceipt(callCtx, txHash)
		},
	)
	if err != nil {
		logger.Error(
			"Failed to get transaction receipt",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.Error(err),
		)
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		logger.Error(
			"Transaction failed",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.String("warpMessageID", signedMessage.ID().String()),
			zap.String("teleporterMessageID", teleporterMessageID.String()),
			zap.String("txHash", txHash.String()),
		)
		return fmt.Errorf("transaction failed with status: %d", receipt.Status)
	}
	return nil
}
