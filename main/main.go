// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

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
	"go.uber.org/atomic"
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

	// Initialize metrics gathered through prometheus
	gatherer, registerer, err := initMetrics()
	if err != nil {
		logger.Fatal("failed to set up prometheus metrics",
			zap.Error(err))
		panic(err)
	}

	// Initialize the global app request network
	logger.Info("Initializing app request network")
	sourceSubnetIDs, sourceBlockchainIDs := cfg.GetSourceIDs()

	// The app request network generates P2P networking logs that are verbose at the info level.
	// Unless the log level is debug or lower, set the network log level to error to avoid spamming the logs.
	networkLogLevel := logging.Error
	if logLevel <= logging.Debug {
		networkLogLevel = logLevel
	}
	network, responseChans, err := peers.NewNetwork(networkLogLevel, registerer, cfg.NetworkID, sourceSubnetIDs, sourceBlockchainIDs, cfg.PChainAPIURL)
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
				// Store the IDs as the cb58 encoding
				var unhealthyRelayers []string
				for id, health := range relayerHealth {
					if !health.Load() {
						unhealthyRelayers = append(unhealthyRelayers, id.String())
					}
				}

				if len(unhealthyRelayers) > 0 {
					return fmt.Errorf("relayers are unhealthy for blockchains %v", unhealthyRelayers)
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

		health := atomic.NewBool(true)
		relayerHealth[blockchainID] = health

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
				health,
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
	shouldProcessMissedBlocks bool,
	relayerHealth *atomic.Bool,
) error {
	logger.Info(
		"Creating relayer",
		zap.String("blockchainID", sourceSubnetInfo.BlockchainID),
	)

	relayer, err := relayer.NewRelayer(
		logger,
		metrics,
		db,
		sourceSubnetInfo,
		pChainClient,
		network,
		responseChan,
		destinationClients,
		messageCreator,
		shouldProcessMissedBlocks,
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
	// Will only return on error or context cancellation
	return relayer.ProcessLogs(ctx)
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
