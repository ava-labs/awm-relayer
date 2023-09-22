// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_eth_client.go -package=mocks

package evm

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"runtime"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	// Set the max fee to twice the estimated base fee.
	// TODO: Revisit this constant factor when we add profit determination, or make it configurable
	BaseFeeFactor        = 2
	MaxPriorityFeePerGas = 2500000000 // 2.5 gwei
)

// Client interface wraps the ethclient.Client interface for mocking purposes.
type Client interface {
	ethclient.Client
}

// Implements DestinationClient
type destinationClient struct {
	client ethclient.Client
	lock   *sync.Mutex

	destinationChainID ids.ID
	pk                 *ecdsa.PrivateKey
	eoa                common.Address
	currentNonce       uint64
	logger             logging.Logger
}

func NewDestinationClient(logger logging.Logger, subnetInfo config.DestinationSubnet) (*destinationClient, error) {
	// Dial the destination RPC endpoint
	client, err := ethclient.Dial(subnetInfo.GetNodeRPCEndpoint())
	if err != nil {
		logger.Error(
			"Failed to dial rpc endpoint",
			zap.Error(err),
		)
		return nil, err
	}

	destinationID, err := ids.FromString(subnetInfo.ChainID)
	if err != nil {
		logger.Error(
			"Could not decode destination chain ID from string",
			zap.Error(err),
		)
		return nil, err
	}

	pk, eoa, err := subnetInfo.GetRelayerAccountInfo()
	if err != nil {
		logger.Error(
			"Could not extract relayer account information from config",
			zap.Error(err),
		)
		return nil, err
	}
	// Explicitly zero the private key when it is gc'd
	runtime.SetFinalizer(pk, func(pk *ecdsa.PrivateKey) {
		pk.D.SetInt64(0)
		pk = nil
	})

	nonce, err := client.NonceAt(context.Background(), eoa, nil)
	if err != nil {
		logger.Error(
			"Failed to get nonce",
			zap.Error(err),
		)
		return nil, err
	}

	return &destinationClient{
		client:             client,
		lock:               new(sync.Mutex),
		destinationChainID: destinationID,
		pk:                 pk,
		eoa:                eoa,
		currentNonce:       nonce,
		logger:             logger,
	}, nil
}

func (c *destinationClient) SendTx(signedMessage *avalancheWarp.Message,
	toAddress string,
	gasLimit uint64,
	callData []byte) error {
	// Synchronize teleporter message requests to the same destination chain so that message ordering is preserved
	c.lock.Lock()
	defer c.lock.Unlock()
	// We need the global 32-byte representation of the destination chain ID, as well as the destination's configured chainID
	// Without the destination's configured chainID, transaction signature verification will fail
	destinationChainIDBigInt, err := c.client.ChainID(context.Background())
	if err != nil {
		c.logger.Error(
			"Failed to get chain ID from destination chain endpoint",
			zap.Error(err),
		)
		return err
	}

	// Get the current base fee estimation, which is based on the previous blocks gas usage.
	baseFee, err := c.client.EstimateBaseFee(context.Background())
	if err != nil {
		c.logger.Error(
			"Failed to get base fee",
			zap.Error(err),
		)
		return err
	}

	// Get the suggested gas tip cap of the network
	// TODO: Add a configurable ceiling to this value
	gasTipCap, err := c.client.SuggestGasTipCap(context.Background())
	if err != nil {
		c.logger.Error(
			"Failed to get gas tip cap",
			zap.Error(err),
		)
		return err
	}

	// Pack the signed message to be delivered in the storage slots.
	// The predicate bytes are packed with a delimiter of 0xff.
	predicateBytes := predicateutils.PackPredicate(signedMessage.Bytes())

	to := common.HexToAddress(toAddress)

	gasFeeCap := baseFee.Mul(baseFee, big.NewInt(BaseFeeFactor))
	gasFeeCap.Add(gasFeeCap, big.NewInt(MaxPriorityFeePerGas))

	// Construct the actual transaction to broadcast on the destination chain
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   destinationChainIDBigInt,
		Nonce:     c.currentNonce,
		To:        &to,
		Gas:       gasLimit,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Value:     big.NewInt(0),
		Data:      callData,
		AccessList: types.AccessList{
			{
				Address:     warp.ContractAddress,
				StorageKeys: predicateutils.BytesToHashSlice(predicateBytes),
			},
		},
	})

	// Sign and send the transaction on the destination chain
	signer := types.LatestSignerForChainID(destinationChainIDBigInt)
	signedTx, err := types.SignTx(tx, signer, c.pk)
	if err != nil {
		c.logger.Error(
			"Failed to sign transaction",
			zap.Error(err),
		)
		return err
	}

	if err := c.client.SendTransaction(context.Background(), signedTx); err != nil {
		c.logger.Error(
			"Failed to send transaction",
			zap.Error(err),
		)
		return err
	}

	// Increment the nonce to use on the destination chain now that we've sent
	// a transaction using the current value.
	c.currentNonce++
	c.logger.Info(
		"Sent transaction",
		zap.String("txID", signedTx.Hash().String()),
	)

	return nil
}

func (c *destinationClient) Client() interface{} {
	return c.client
}

func (c *destinationClient) SenderAddress() common.Address {
	return c.eoa
}

func (c *destinationClient) DestinationChainID() ids.ID {
	return c.destinationChainID
}
