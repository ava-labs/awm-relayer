// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"errors"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
)

const (
	receiveMessageGasLimitBufferAmount = 100_000
)

var (
	errRequiredGasLimitTooHigh = errors.New("required gas limit too high")
)

// CalculateReceiveMessageGasLimit calculates the estimated gas amount used by a single call
// to receiveCrossChainMessage for the given message and validator bit vector. The result amount
// depends on the required limit for the message execution, the number of validator signatures
// included in the aggregate signature, the static gas cost defined by the precompile, and an
// extra buffer amount defined here to ensure the call doesn't run out of gas.
func CalculateReceiveMessageGasLimit(numSigners int, executionRequiredGasLimit *big.Int) (uint64, error) {
	if !executionRequiredGasLimit.IsUint64() {
		return 0, errRequiredGasLimitTooHigh
	}

	gasAmounts := []uint64{
		executionRequiredGasLimit.Uint64(),
		utils.ReceiveCrossChainMessageStaticGasCost,
		uint64(numSigners) * utils.ReceiveCrossChainMessageGasCostPerAggregatedKey,
		receiveMessageGasLimitBufferAmount,
	}

	res := gasAmounts[0]
	var err error
	for i := 1; i < len(gasAmounts); i++ {
		res, err = math.Add64(res, gasAmounts[i])
		if err != nil {
			return 0, err
		}
	}

	return res, nil
}

// Pack the SendCrossChainMessage event type. PackEvent is documented as not supporting struct types, so this should be used
// with caution. Here, we only use it for testing purposes. In a real setting, the Teleporter contract should pack the event.
func PackTeleporterMessage(destinationChainID common.Hash, message TeleporterMessage) ([]byte, error) {
	_, hashes, err := EVMTeleporterContractABI.PackEvent("SendCrossChainMessage", destinationChainID, message.MessageID, message)
	return hashes, err
}
