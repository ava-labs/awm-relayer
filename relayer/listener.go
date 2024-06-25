// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/ethclient"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

const (
	maxSubscribeAttempts = 10
	// TODO attempt to resubscribe in perpetuity once we are able to process missed blocks and
	// refresh the chain config on reconnect.
	maxResubscribeAttempts = 10
)

// Listener handles all messages sent from a given source chain
type Listener struct {
	Subscriber         vms.Subscriber
	currentRequestID   uint32
	contractMessage    vms.ContractMessage
	logger             logging.Logger
	sourceBlockchain   config.SourceBlockchain
	catchUpResultChan  chan bool
	healthStatus       *atomic.Bool
	ethClient          ethclient.Client
	messageCoordinator *MessageCoordinator
}

// runListener creates a Listener instance and the ApplicationRelayers for a subnet.
// The Listener listens for warp messages on that subnet, and the ApplicationRelayers handle delivery to the destination
func RunListener(
	ctx context.Context,
	logger logging.Logger,
	sourceBlockchain config.SourceBlockchain,
	ethRPCClient ethclient.Client,
	relayerHealth *atomic.Bool,
	processMissedBlocks bool,
	minHeight uint64,
	messageCoordinator *MessageCoordinator,
) error {
	// Create the Listener
	listener, err := newListener(
		ctx,
		logger,
		sourceBlockchain,
		ethRPCClient,
		relayerHealth,
		processMissedBlocks,
		minHeight,
		messageCoordinator,
	)
	if err != nil {
		return fmt.Errorf("failed to create listener instance: %w", err)
	}

	logger.Info(
		"Listener initialized. Listening for messages to relay.",
		zap.String("originBlockchainID", sourceBlockchain.BlockchainID),
	)

	// Wait for logs from the subscribed node
	// Will only return on error or context cancellation
	return listener.processLogs(ctx)
}

func newListener(
	ctx context.Context,
	logger logging.Logger,
	sourceBlockchain config.SourceBlockchain,
	ethRPCClient ethclient.Client,
	relayerHealth *atomic.Bool,
	processMissedBlocks bool,
	startingHeight uint64,
	messageCoordinator *MessageCoordinator,
) (*Listener, error) {
	blockchainID, err := ids.FromString(sourceBlockchain.BlockchainID)
	if err != nil {
		logger.Error(
			"Invalid blockchainID provided to subscriber",
			zap.Error(err),
		)
		return nil, err
	}
	ethWSClient, err := utils.DialWithConfig(
		ctx,
		sourceBlockchain.WSEndpoint.BaseURL,
		sourceBlockchain.WSEndpoint.HTTPHeaders,
		sourceBlockchain.WSEndpoint.QueryParams,
	)
	if err != nil {
		logger.Error(
			"Failed to connect to node via WS",
			zap.String("blockchainID", blockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	sub := vms.NewSubscriber(logger, config.ParseVM(sourceBlockchain.VM), blockchainID, ethWSClient)

	// Marks when the listener has finished the catch-up process on startup.
	// Until that time, we do not know the order in which messages are processed,
	// since the catch-up process occurs concurrently with normal message processing
	// via the subscriber's Subscribe method. As a result, we cannot safely write the
	// latest processed block to the database without risking missing a block in a fault
	// scenario.
	catchUpResultChan := make(chan bool, 1)

	logger.Info(
		"Creating relayer",
		zap.String("subnetID", sourceBlockchain.GetSubnetID().String()),
		zap.String("subnetIDHex", sourceBlockchain.GetSubnetID().Hex()),
		zap.String("blockchainID", sourceBlockchain.GetBlockchainID().String()),
		zap.String("blockchainIDHex", sourceBlockchain.GetBlockchainID().Hex()),
	)
	lstnr := Listener{
		Subscriber:         sub,
		currentRequestID:   rand.Uint32(), // Initialize to a random value to mitigate requestID collision
		contractMessage:    vms.NewContractMessage(logger, sourceBlockchain),
		logger:             logger,
		sourceBlockchain:   sourceBlockchain,
		catchUpResultChan:  catchUpResultChan,
		healthStatus:       relayerHealth,
		ethClient:          ethRPCClient,
		messageCoordinator: messageCoordinator,
	}

	// Open the subscription. We must do this before processing any missed messages, otherwise we may miss an incoming message
	// in between fetching the latest block and subscribing.
	err = lstnr.Subscriber.Subscribe(maxSubscribeAttempts)
	if err != nil {
		logger.Error(
			"Failed to subscribe to node",
			zap.Error(err),
		)
		return nil, err
	}

	if processMissedBlocks {
		// Process historical blocks in a separate goroutine so that the main processing loop can
		// start processing new blocks as soon as possible. Otherwise, it's possible for
		// ProcessFromHeight to overload the message queue and cause a deadlock.
		go sub.ProcessFromHeight(big.NewInt(0).SetUint64(startingHeight), lstnr.catchUpResultChan)
	} else {
		lstnr.logger.Info(
			"processed-missed-blocks set to false, starting processing from chain head",
			zap.String("blockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
		)
		lstnr.catchUpResultChan <- true
	}

	return &lstnr, nil
}

// Listens to the Subscriber logs channel to process them.
// On subscriber error, attempts to reconnect and errors if unable.
// Exits if context is cancelled by another goroutine.
func (lstnr *Listener) processLogs(ctx context.Context) error {
	// Error channel for application relayer errors
	errChan := make(chan error)
	for {
		select {
		case err := <-errChan:
			lstnr.healthStatus.Store(false)
			lstnr.logger.Error(
				"Received error from application relayer",
				zap.Error(err),
			)
		case catchUpResult, ok := <-lstnr.catchUpResultChan:
			// As soon as we've received anything on the channel, there are no more values expected.
			// The expected case is that the channel is closed by the subscriber after writing a value to it,
			// but we also defensively handle an unexpected close.
			lstnr.catchUpResultChan = nil

			// Mark the relayer as unhealthy if the catch-up process fails or if the catch-up channel is unexpectedly closed.
			if !ok {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Catch-up channel unexpectedly closed. Exiting listener goroutine.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("catch-up channel unexpectedly closed")
			}
			if !catchUpResult {
				lstnr.healthStatus.Store(false)
				lstnr.logger.Error(
					"Failed to catch up on historical blocks. Exiting listener goroutine.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				)
				return fmt.Errorf("failed to catch up on historical blocks")
			}
		case blockHeader := <-lstnr.Subscriber.Headers():
			go lstnr.messageCoordinator.processBlock(blockHeader, lstnr.ethClient, errChan)
		case err := <-lstnr.Subscriber.Err():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Error(
				"Received error from subscribed node",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
				zap.Error(err),
			)
			// TODO try to resubscribe in perpetuity once we have a mechanism for refreshing state
			// variables such as Quorum values and processing missed blocks.
			err = lstnr.reconnectToSubscriber()
			if err != nil {
				lstnr.logger.Error(
					"Relayer goroutine exiting.",
					zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
					zap.Error(err),
				)
				return fmt.Errorf("listener goroutine exiting: %w", err)
			}
		case <-ctx.Done():
			lstnr.healthStatus.Store(false)
			lstnr.logger.Info(
				"Exiting listener because context cancelled",
				zap.String("sourceBlockchainID", lstnr.sourceBlockchain.GetBlockchainID().String()),
			)
			return nil
		}
	}
}

// Sets the listener health status to false while attempting to reconnect.
func (lstnr *Listener) reconnectToSubscriber() error {
	// Attempt to reconnect the subscription
	err := lstnr.Subscriber.Subscribe(maxResubscribeAttempts)
	if err != nil {
		return fmt.Errorf("failed to resubscribe to node: %w", err)
	}

	// Success
	lstnr.healthStatus.Store(true)
	return nil
}
