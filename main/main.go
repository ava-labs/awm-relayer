// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/alexliesenfeld/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	defaultApiPort     = 8080
	defaultMetricsPort = 9090
)

func main() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])
	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	cfg, optionOverwritten, err := config.BuildConfig(v)
	if err != nil {
		fmt.Printf("couldn't build config: %s\n", err)
		os.Exit(1)
	}

	logLevel, err := logging.ToLevel(cfg.LogLevel)
	if err != nil {
		fmt.Printf("error with log level: %v", err)
	}

	logger := logging.NewLogger(
		"awm-relayer",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	logger.Info("Initializing awm-relayer")
	overwrittenLog := ""
	if optionOverwritten {
		overwrittenLog = " Some options were overwritten"
	}
	logger.Info(fmt.Sprintf("Set config options.%s", overwrittenLog))

	// Global P-Chain client used to get subnet validator sets
	pChainClient := platformvm.NewClient(cfg.PChainAPIURL)

	// Initialize all destination clients
	logger.Info("Initializing destination clients")
	destinationClients, err := vms.CreateDestinationClients(logger, cfg)
	if err != nil {
		logger.Error(
			"Failed to create destination clients",
			zap.Error(err),
		)
		return
	}

	// Initialize the global app request network
	logger.Info("Initializing app request network")
	sourceSubnetIDs, sourceBlockchainIDs := cfg.GetSourceIDs()

	// Initialize metrics gathered through prometheus
	gatherer, registerer, err := initMetrics()
	if err != nil {
		logger.Fatal("failed to set up prometheus metrics",
			zap.Error(err))
		panic(err)
	}

	network, responseChans, err := peers.NewNetwork(logger, registerer, cfg.NetworkID, sourceSubnetIDs, sourceBlockchainIDs, cfg.PChainAPIURL)
	if err != nil {
		logger.Error(
			"Failed to create app request network",
			zap.Error(err),
		)
		return
	}

	// Each goroutine will have an atomic bool that it can set to false if it ever disconnects from its subscription.
	relayerHealth := make(map[ids.ID]*atomic.Bool)

	checker := health.NewChecker(
		health.WithCheck(health.Check{
			Name: "relayers-all",
			Check: func(context.Context) error {
				var unhealthlyRelayers []string
				for id, health := range relayerHealth {
					if !health.Load() {
						unhealthlyRelayers = append(unhealthlyRelayers, id.Hex())
					}
				}

				if len(unhealthlyRelayers) > 0 {
					return fmt.Errorf("relayers %v are unhealthy", unhealthlyRelayers)
				}
				return nil
			},
		}),
	)

	http.Handle("/health", health.NewHandler(checker))

	// start the health check server
	go func() {
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", defaultApiPort), nil))
	}()

	startMetricsServer(logger, gatherer, defaultMetricsPort)

	metrics := relayer.NewMessageRelayerMetrics(registerer)

	// Initialize message creator passed down to relayers for creating app requests.
	messageCreator, err := message.NewCreator(logger, registerer, "message_creator", constants.DefaultNetworkCompressionType, constants.DefaultNetworkMaximumInboundTimeout)
	if err != nil {
		logger.Error(
			"Failed to create message creator",
			zap.Error(err),
		)
		return
	}

	// Initialize the database
	db, err := database.NewJSONFileStorage(logger, cfg.StorageLocation, sourceBlockchainIDs)
	if err != nil {
		logger.Error(
			"Failed to create database",
			zap.Error(err),
		)
		return
	}

	// Create relayers for each of the subnets configured as a source
	errGroup, ctx := errgroup.WithContext(context.Background())
	for _, s := range cfg.SourceSubnets {
		blockchainID, err := ids.FromString(s.BlockchainID)
		if err != nil {
			logger.Error(
				"Invalid subnetID in configuration",
				zap.Error(err),
			)
			return
		}
		subnetInfo := s

		health := atomic.Bool{}
		health.Store(true)
		relayerHealth[blockchainID] = &health

		// errgroup will cancel the context when the first goroutine returns an error
		errGroup.Go(func() error {
			// runRelayer runs until it errors or the context is cancelled by another goroutine
			return runRelayer(
				ctx,
				logger,
				metrics,
				db,
				subnetInfo,
				pChainClient,
				network,
				responseChans[blockchainID],
				destinationClients,
				messageCreator,
				cfg.ProcessMissedBlocks,
				&health,
			)
		})
	}
	err = errGroup.Wait()
	logger.Error(
		"Relayer exiting.",
		zap.Error(err),
	)
}

// runRelayer creates a relayer instance for a subnet. It listens for warp messages on that subnet, and handles delivery to the destination
func runRelayer(
	ctx context.Context,
	logger logging.Logger,
	metrics *relayer.MessageRelayerMetrics,
	db database.RelayerDatabase,
	sourceSubnetInfo config.SourceSubnet,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	processMissedBlocks bool,
	relayerHealth *atomic.Bool,
) error {
	logger.Info(
		"Creating relayer",
		zap.String("blockchainID", sourceSubnetInfo.BlockchainID),
	)

	relayer, err := relayer.NewRelayer(
		logger,
		db,
		sourceSubnetInfo,
		pChainClient,
		network,
		responseChan,
		destinationClients,
		processMissedBlocks,
		relayerHealth,
	)
	if err != nil {
		return fmt.Errorf("Failed to create relayer instance: %w", err)
	}
	logger.Info(
		"Created relayer. Listening for messages to relay.",
		zap.String("blockchainID", sourceSubnetInfo.BlockchainID),
	)

	// Wait for logs from the subscribed node
	for {
		select {
		case txLog := <-relayer.Subscriber.Logs():
			logger.Info(
				"Handling Teleporter submit message log.",
				zap.String("txId", hex.EncodeToString(txLog.SourceTxID)),
				zap.String("originChainId", sourceSubnetInfo.BlockchainID),
				zap.String("sourceAddress", txLog.SourceAddress.String()),
			)

			// Relay the message to the destination chain. Continue on failure.
			err = relayer.RelayMessage(&txLog, metrics, messageCreator)
			if err != nil {
				logger.Error(
					"Error relaying message",
					zap.String("originChainID", sourceSubnetInfo.BlockchainID),
					zap.Error(err),
				)
				continue
			}
		case err := <-relayer.Subscriber.Err():
			logger.Error(
				"Received error from subscribed node",
				zap.String("originChainID", sourceSubnetInfo.BlockchainID),
				zap.Error(err),
			)
			err = relayer.ReconnectToSubscriber()
			if err != nil {
				return fmt.Errorf("exiting relayer goroutine: %w", err)
			}
		case <-ctx.Done():
			relayerHealth.Store(false)
			logger.Info(
				"Exiting Relayer because context cancelled",
				zap.String("originChainId", sourceSubnetInfo.BlockchainID),
			)
			return nil
		}
	}
}

func startMetricsServer(logger logging.Logger, gatherer prometheus.Gatherer, port uint32) {
	http.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))

	go func() {
		logger.Info("starting metrics server...",
			zap.Uint32("port", port))
		err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
		if err != nil {
			logger.Fatal("metrics server exited",
				zap.Error(err),
				zap.Uint32("port", port))
			panic(err)
		}
	}()
}

func initMetrics() (prometheus.Gatherer, prometheus.Registerer, error) {
	gatherer := metrics.NewMultiGatherer()
	registry := prometheus.NewRegistry()
	if err := gatherer.Register("app", registry); err != nil {
		return nil, nil, err
	}
	return gatherer, registry, nil
}
