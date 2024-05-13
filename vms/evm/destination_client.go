// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_eth_client.go -package=mocks

package evm

import (
	"context"
	"math/big"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/ethclient"
	"github.com/ava-labs/awm-relayer/vms/evm/signer"
	"github.com/ava-labs/subnet-evm/core/types"
	evmethclient "github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	predicateutils "github.com/ava-labs/subnet-evm/predicate"
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
	evmethclient.Client
}

// Implements DestinationClient
type destinationClient struct {
	client                  evmethclient.Client
	lock                    *sync.Mutex
	destinationBlockchainID ids.ID
	signer                  signer.Signer
	evmChainID              *big.Int
	currentNonce            uint64
	logger                  logging.Logger
}

func NewDestinationClient(logger logging.Logger, destinationBlockchain *config.DestinationBlockchain) (*destinationClient, error) {
	// Dial the destination RPC endpoint
	client, err := ethclient.DialWithConfig(
		context.Background(),
		destinationBlockchain.RPCEndpoint.BaseURL,
		destinationBlockchain.RPCEndpoint.HTTPHeaders,
		destinationBlockchain.RPCEndpoint.QueryParams,
	)
	if err != nil {
		logger.Error(
			"Failed to dial rpc endpoint",
			zap.Error(err),
		)
		return nil, err
	}

	destinationID, err := ids.FromString(destinationBlockchain.BlockchainID)
	if err != nil {
		logger.Error(
			"Could not decode destination chain ID from string",
			zap.Error(err),
		)
		return nil, err
	}

	sgnr, err := signer.NewSigner(destinationBlockchain)
	if err != nil {
		logger.Error(
			"Failed to create signer",
			zap.Error(err),
		)
		return nil, err
	}

	nonce, err := client.NonceAt(context.Background(), sgnr.Address(), nil)
	if err != nil {
		logger.Error(
			"Failed to get nonce",
			zap.Error(err),
		)
		return nil, err
	}

	evmChainID, err := client.ChainID(context.Background())
	if err != nil {
		logger.Error(
			"Failed to get chain ID from destination chain endpoint",
			zap.Error(err),
		)
		return nil, err
	}

	return &destinationClient{
		client:                  client,
		lock:                    new(sync.Mutex),
		destinationBlockchainID: destinationID,
		signer:                  sgnr,
		evmChainID:              evmChainID,
		currentNonce:            nonce,
		logger:                  logger,
	}, nil
}

func (c *destinationClient) SendTx(signedMessage *avalancheWarp.Message,
	toAddress string,
	gasLimit uint64,
	callData []byte,
) error {
	// Synchronize teleporter message requests to the same destination chain so that message ordering is preserved
	c.lock.Lock()
	defer c.lock.Unlock()

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

	to := common.HexToAddress(toAddress)
	gasFeeCap := baseFee.Mul(baseFee, big.NewInt(BaseFeeFactor))
	gasFeeCap.Add(gasFeeCap, big.NewInt(MaxPriorityFeePerGas))

	// Construct the actual transaction to broadcast on the destination chain
	tx := predicateutils.NewPredicateTx(
		c.evmChainID,
		c.currentNonce,
		&to,
		gasLimit,
		gasFeeCap,
		gasTipCap,
		big.NewInt(0),
		callData,
		types.AccessList{},
		warp.ContractAddress,
		signedMessage.Bytes(),
	)

	// Sign and send the transaction on the destination chain
	signedTx, err := c.signer.SignTx(tx, c.evmChainID)
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
	return c.signer.Address()
}

func (c *destinationClient) DestinationBlockchainID() ids.ID {
	return c.destinationBlockchainID
}
