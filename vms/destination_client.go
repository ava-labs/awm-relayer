// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// DestinationClient is the interface for the destination chain client. Methods that interact with the destination chain
// should generally be implemented in a thread safe way, as they will be called concurrently by the message relayers.
type DestinationClient interface {
	// SendTx contructs the transaction from warp primitives, and send to the configured destination chain endpoint
	// TODO: Make generic for any VM.
	SendTx(signedMessage *warp.Message, toAddress string, gasLimit uint64, callData []byte) error

	// Client returns the underlying client for the destination chain
	Client() interface{}

	// SenderAddress returns the address of the relayer on the destination chain
	SenderAddress() common.Address

	// DestinationChainID returns the ID of the destination chain
	DestinationChainID() ids.ID
}

func NewDestinationClient(logger logging.Logger, subnetInfo config.DestinationSubnet) (DestinationClient, error) {
	switch config.ParseVM(subnetInfo.VM) {
	case config.EVM:
		return evm.NewDestinationClient(logger, subnetInfo)
	default:
		return nil, fmt.Errorf("invalid vm")
	}
}

// CreateDestinationClients creates destination clients for all subnets configured as destinations
func CreateDestinationClients(logger logging.Logger, relayerConfig config.Config) (map[ids.ID]DestinationClient, error) {
	destinationClients := make(map[ids.ID]DestinationClient)
	for _, s := range relayerConfig.DestinationSubnets {
		chainID, err := ids.FromString(s.ChainID)
		if err != nil {
			logger.Error(
				"Failed to decode base-58 encoded source chain ID",
				zap.Error(err),
			)
			return nil, err
		}
		if _, ok := destinationClients[chainID]; ok {
			logger.Info(
				"Destination client already found for chainID. Continuing",
				zap.String("chainID", chainID.String()),
			)
			continue
		}

		destinationClient, err := NewDestinationClient(logger, s)
		if err != nil {
			logger.Error(
				"Could not create destination client",
				zap.Error(err),
			)
			return nil, err
		}

		destinationClients[chainID] = destinationClient
	}
	return destinationClients, nil
}
