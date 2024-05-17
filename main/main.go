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

	"github.com/alexliesenfeld/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var version = "v0.0.0-dev"

func main() {
	fs := config.BuildFlagSet()
	if err := fs.Parse(os.Args[1:]); err != nil {
		config.DisplayUsageText()
		panic(fmt.Errorf("couldn't parse flags: %w", err))
	}
	// If the version flag is set, display the version then exit
	displayVersion, err := fs.GetBool(config.VersionKey)
	if err != nil {
		panic(fmt.Errorf("error reading %s flag value: %w", config.VersionKey, err))
	}
	if displayVersion {
		fmt.Printf("%s\n", version)
		os.Exit(0)
	}
	// If the help flag is set, output the usage text then exit
	help, err := fs.GetBool(config.HelpKey)
	if err != nil {
		panic(fmt.Errorf("error reading %s flag value: %w", config.HelpKey, err))
	}
	if help {
		config.DisplayUsageText()
		os.Exit(0)
	}

	v, err := config.BuildViper(fs)
	if err != nil {
		panic(fmt.Errorf("couldn't configure flags: %w", err))
	}

	cfg, optionOverwritten, err := config.BuildConfig(v)
	if err != nil {
		panic(fmt.Errorf("couldn't build config: %w", err))
	}
	// Initialize the Warp Quorum values by fetching via RPC
	// We do this here so that BuildConfig doesn't need to make RPC calls
	if err = cfg.InitializeWarpQuorums(); err != nil {
		panic(fmt.Errorf("couldn't initialize warp quorums: %w", err))
	}

	logLevel, err := logging.ToLevel(cfg.LogLevel)
	if err != nil {
		panic(fmt.Errorf("error with log level: %w", err))
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

	// Initialize all destination clients
	logger.Info("Initializing destination clients")
	destinationClients, err := vms.CreateDestinationClients(logger, cfg)
	if err != nil {
		logger.Error(
			"Failed to create destination clients",
			zap.Error(err),
		)
		panic(err)
	}

	// Initialize metrics gathered through prometheus
	gatherer, registerer, err := initializeMetrics()
	if err != nil {
		logger.Fatal("Failed to set up prometheus metrics",
			zap.Error(err))
		panic(err)
	}

	// Initialize the global app request network
	logger.Info("Initializing app request network")

	// The app request network generates P2P networking logs that are verbose at the info level.
	// Unless the log level is debug or lower, set the network log level to error to avoid spamming the logs.
	networkLogLevel := logging.Error
	if logLevel <= logging.Debug {
		networkLogLevel = logLevel
	}
	network, responseChans, err := peers.NewNetwork(
		networkLogLevel,
		registerer,
		&cfg,
	)
	if err != nil {
		logger.Error(
			"Failed to create app request network",
			zap.Error(err),
		)
		panic(err)
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
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", cfg.APIPort), nil))
	}()

	startMetricsServer(logger, gatherer, cfg.MetricsPort)

	metrics, err := relayer.NewApplicationRelayerMetrics(registerer)
	if err != nil {
		logger.Error(
			"Failed to create application relayer metrics",
			zap.Error(err),
		)
		panic(err)
	}

	// Initialize message creator passed down to relayers for creating app requests.
	messageCreator, err := message.NewCreator(logger, registerer, "message_creator", constants.DefaultNetworkCompressionType, constants.DefaultNetworkMaximumInboundTimeout)
	if err != nil {
		logger.Error(
			"Failed to create message creator",
			zap.Error(err),
		)
		panic(err)
	}

	// Initialize the database
	db, err := database.NewDatabase(logger, &cfg)
	if err != nil {
		logger.Error(
			"Failed to create database",
			zap.Error(err),
		)
		panic(err)
	}

	// Initialize the global write ticker
	ticker := utils.NewTicker(cfg.DBWriteIntervalSeconds)
	go ticker.Run()

	manualWarpMessages := make(map[ids.ID][]*relayerTypes.WarpLogInfo)
	for _, msg := range cfg.ManualWarpMessages {
		sourceBlockchainID := msg.GetSourceBlockchainID()

		warpLogInfo := relayerTypes.WarpLogInfo{
			SourceAddress:    msg.GetSourceAddress(),
			UnsignedMsgBytes: msg.GetUnsignedMessageBytes(),
		}
		manualWarpMessages[sourceBlockchainID] = append(manualWarpMessages[sourceBlockchainID], &warpLogInfo)
	}

	// Create relayers for each of the subnets configured as a source
	errGroup, ctx := errgroup.WithContext(context.Background())
	for _, s := range cfg.SourceBlockchains {
		blockchainID, err := ids.FromString(s.BlockchainID)
		if err != nil {
			logger.Error(
				"Invalid subnetID in configuration",
				zap.Error(err),
			)
			panic(err)
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
				ticker,
				*subnetInfo,
				network,
				responseChans[blockchainID],
				destinationClients,
				messageCreator,
				health,
				manualWarpMessages[blockchainID],
				&cfg,
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
	metrics *relayer.ApplicationRelayerMetrics,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	sourceSubnetInfo config.SourceBlockchain,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
	relayerHealth *atomic.Bool,
	manualWarpMessages []*relayerTypes.WarpLogInfo,
	cfg *config.Config,
) error {
	logger.Info(
		"Creating relayer",
		zap.String("originBlockchainID", sourceSubnetInfo.BlockchainID),
	)

	listener, err := relayer.NewListener(
		logger,
		metrics,
		db,
		ticker,
		sourceSubnetInfo,
		network,
		responseChan,
		destinationClients,
		messageCreator,
		relayerHealth,
		cfg,
	)
	if err != nil {
		return fmt.Errorf("failed to create listener instance: %w", err)
	}
	logger.Info(
		"Created listener",
		zap.String("blockchainID", sourceSubnetInfo.BlockchainID),
	)

	// Send any messages that were specified in the configuration
	for _, warpMessage := range manualWarpMessages {
		logger.Info(
			"Relaying manual Warp message",
			zap.String("blockchainID", sourceSubnetInfo.BlockchainID),
			zap.String("warpMessageBytes", hex.EncodeToString(warpMessage.UnsignedMsgBytes)),
		)
		err := listener.RouteManualWarpMessage(warpMessage)
		if err != nil {
			logger.Error(
				"Failed to relay manual Warp message. Continuing.",
				zap.Error(err),
				zap.String("warpMessageBytes", hex.EncodeToString(warpMessage.UnsignedMsgBytes)),
			)
			continue
		}
	}

	logger.Info(
		"Listener initialized. Listening for messages to relay.",
		zap.String("originBlockchainID", sourceSubnetInfo.BlockchainID),
	)

	// Wait for logs from the subscribed node
	// Will only return on error or context cancellation
	return listener.ProcessLogs(ctx)
}

func startMetricsServer(logger logging.Logger, gatherer prometheus.Gatherer, port uint16) {
	http.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))

	go func() {
		logger.Info("starting metrics server...",
			zap.Uint16("port", port))
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()
}

func initializeMetrics() (prometheus.Gatherer, prometheus.Registerer, error) {
	gatherer := metrics.NewMultiGatherer()
	registry := prometheus.NewRegistry()
	if err := gatherer.Register("app", registry); err != nil {
		return nil, nil, err
	}
	return gatherer, registry, nil
}
