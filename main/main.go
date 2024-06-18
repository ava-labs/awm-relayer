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
	"strings"

	"github.com/ava-labs/awm-relayer/messages"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/messages/teleporter"

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
	"github.com/ava-labs/awm-relayer/types"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
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

	cfg, err := config.NewConfig(v)
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
	if cfg.HasOverwrittenOptions() {
		overwrittenLog = fmt.Sprintf(" Some options were overwritten: %s", strings.Join(cfg.GetOverwrittenOptions(), ", "))
	}
	logger.Info(fmt.Sprintf("Set config options.%s", overwrittenLog))

	// Initialize all destination clients
	logger.Info("Initializing destination clients")
	destinationClients, err := vms.CreateDestinationClients(logger, cfg)
	if err != nil {
		logger.Fatal("Failed to create destination clients", zap.Error(err))
		panic(err)
	}

	// Initialize all source clients
	logger.Info("Initializing destination clients")
	sourceClients, err := createSourceClients(context.Background(), logger, &cfg)
	if err != nil {
		logger.Fatal("Failed to create source clients", zap.Error(err))
		panic(err)
	}

	// Initialize metrics gathered through prometheus
	gatherer, registerer, err := initializeMetrics()
	if err != nil {
		logger.Fatal("Failed to set up prometheus metrics", zap.Error(err))
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
	network, err := peers.NewNetwork(
		networkLogLevel,
		registerer,
		&cfg,
	)
	if err != nil {
		logger.Fatal("Failed to create app request network", zap.Error(err))
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

	startMetricsServer(logger, gatherer, cfg.MetricsPort)

	relayerMetrics, err := relayer.NewApplicationRelayerMetrics(registerer)
	if err != nil {
		logger.Fatal("Failed to create application relayer metrics", zap.Error(err))
		panic(err)
	}

	// Initialize message creator passed down to relayers for creating app requests.
	messageCreator, err := message.NewCreator(logger, registerer, "message_creator", constants.DefaultNetworkCompressionType, constants.DefaultNetworkMaximumInboundTimeout)
	if err != nil {
		logger.Fatal("Failed to create message creator", zap.Error(err))
		panic(err)
	}

	// Initialize the database
	db, err := database.NewDatabase(logger, &cfg)
	if err != nil {
		logger.Fatal("Failed to create database", zap.Error(err))
		panic(err)
	}

	// Initialize the global write ticker
	ticker := utils.NewTicker(cfg.DBWriteIntervalSeconds)
	go ticker.Run()

	messageHandlerFactories, err := createMessageHandlerFactories(logger, &cfg)
	if err != nil {
		logger.Fatal("Failed to create Message Handler Factories", zap.Error(err))
		panic(err)
	}

	applicationRelayers, minHeights, err := createApplicationRelayers(
		context.Background(),
		logger,
		relayerMetrics,
		db,
		ticker,
		network,
		messageCreator,
		&cfg,
		sourceClients,
		destinationClients,
	)
	if err != nil {
		logger.Fatal("Failed to create Application Relayers", zap.Error(err))
		panic(err)
	}
	relayer.SetMessageCoordinator(logger, messageHandlerFactories, applicationRelayers, sourceClients)

	// Initialize the API after the message coordinator is set
	http.Handle("/health", health.NewHandler(checker))
	http.HandleFunc(relayer.RelayMessageApiPath, relayer.RelayMessageAPIHandler)

	// start the health check server
	go func() {
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", cfg.APIPort), nil))
	}()

	// Gather manual Warp messages specified in the configuration
	manualWarpMessages := make(map[ids.ID][]*relayerTypes.WarpMessageInfo)
	for _, msg := range cfg.ManualWarpMessages {
		sourceBlockchainID := msg.GetSourceBlockchainID()
		unsignedMsg, err := types.UnpackWarpMessage(msg.GetUnsignedMessageBytes())
		if err != nil {
			logger.Fatal(
				"Failed to unpack manual Warp message",
				zap.String("warpMessageBytes", hex.EncodeToString(msg.GetUnsignedMessageBytes())),
				zap.Error(err),
			)
			panic(err)
		}
		warpLogInfo := relayerTypes.WarpMessageInfo{
			SourceAddress:   msg.GetSourceAddress(),
			UnsignedMessage: unsignedMsg,
		}
		manualWarpMessages[sourceBlockchainID] = append(manualWarpMessages[sourceBlockchainID], &warpLogInfo)
	}

	// Create listeners for each of the subnets configured as a source
	errGroup, ctx := errgroup.WithContext(context.Background())
	for _, s := range cfg.SourceBlockchains {
		sourceBlockchain := s

		isHealthy := atomic.NewBool(true)
		relayerHealth[s.GetBlockchainID()] = isHealthy

		errGroup.Go(func() error {
			return relayer.ProcessManualWarpMessages(manualWarpMessages[sourceBlockchain.GetBlockchainID()])
		})

		// errgroup will cancel the context when the first goroutine returns an error
		errGroup.Go(func() error {
			// runListener runs until it errors or the context is cancelled by another goroutine
			return relayer.RunListener(
				ctx,
				logger,
				*sourceBlockchain,
				sourceClients[sourceBlockchain.GetBlockchainID()],
				isHealthy,
				cfg.ProcessMissedBlocks,
				minHeights[sourceBlockchain.GetBlockchainID()],
			)
		})
	}
	err = errGroup.Wait()
	logger.Error("Relayer exiting.", zap.Error(err))
}

func createMessageHandlerFactories(
	logger logging.Logger,
	globalConfig *config.Config,
) (map[ids.ID]map[common.Address]messages.MessageHandlerFactory, error) {
	messageHandlerFactories := make(map[ids.ID]map[common.Address]messages.MessageHandlerFactory)
	for _, sourceBlockchain := range globalConfig.SourceBlockchains {
		messageHandlerFactoriesForSource := make(map[common.Address]messages.MessageHandlerFactory)
		// Create message managers for each supported message protocol
		for addressStr, cfg := range sourceBlockchain.MessageContracts {
			address := common.HexToAddress(addressStr)
			format := cfg.MessageFormat
			var (
				m   messages.MessageHandlerFactory
				err error
			)
			switch config.ParseMessageProtocol(format) {
			case config.TELEPORTER:
				m, err = teleporter.NewMessageHandlerFactory(
					logger,
					address,
					cfg,
				)
			case config.OFF_CHAIN_REGISTRY:
				m, err = offchainregistry.NewMessageHandlerFactory(
					logger,
					cfg,
				)
			default:
				m, err = nil, fmt.Errorf("invalid message format %s", format)
			}
			if err != nil {
				logger.Error("Failed to create message manager", zap.Error(err))
				return nil, err
			}
			messageHandlerFactoriesForSource[address] = m
		}
		messageHandlerFactories[sourceBlockchain.GetBlockchainID()] = messageHandlerFactoriesForSource
	}
	return messageHandlerFactories, nil
}

func createSourceClients(
	ctx context.Context,
	logger logging.Logger,
	cfg *config.Config,
) (map[ids.ID]ethclient.Client, error) {
	var err error
	clients := make(map[ids.ID]ethclient.Client)

	for _, sourceBlockchain := range cfg.SourceBlockchains {
		clients[sourceBlockchain.GetBlockchainID()], err = utils.DialWithConfig(
			ctx,
			sourceBlockchain.RPCEndpoint.BaseURL,
			sourceBlockchain.RPCEndpoint.HTTPHeaders,
			sourceBlockchain.RPCEndpoint.QueryParams,
		)
		if err != nil {
			logger.Error(
				"Failed to connect to node via RPC",
				zap.String("blockchainID", sourceBlockchain.BlockchainID),
				zap.Error(err),
			)
			return nil, err
		}
	}
	return clients, nil
}

// Returns a map of application relayers, as well as a map of source blockchain IDs to starting heights.
func createApplicationRelayers(
	ctx context.Context,
	logger logging.Logger,
	relayerMetrics *relayer.ApplicationRelayerMetrics,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	network *peers.AppRequestNetwork,
	messageCreator message.Creator,
	cfg *config.Config,
	sourceClients map[ids.ID]ethclient.Client,
	destinationClients map[ids.ID]vms.DestinationClient,
) (map[common.Hash]*relayer.ApplicationRelayer, map[ids.ID]uint64, error) {
	applicationRelayers := make(map[common.Hash]*relayer.ApplicationRelayer)
	minHeights := make(map[ids.ID]uint64)
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		currentHeight, err := sourceClients[sourceBlockchain.GetBlockchainID()].BlockNumber(ctx)
		if err != nil {
			logger.Error("Failed to get current block height", zap.Error(err))
			return nil, nil, err
		}

		// Create the ApplicationRelayers
		applicationRelayersForSource, minHeight, err := createApplicationRelayersForSourceChain(
			ctx,
			logger,
			relayerMetrics,
			db,
			ticker,
			*sourceBlockchain,
			network,
			messageCreator,
			cfg,
			currentHeight,
			destinationClients,
		)
		if err != nil {
			logger.Error(
				"Failed to create application relayers",
				zap.String("blockchainID", sourceBlockchain.BlockchainID),
				zap.Error(err),
			)
			return nil, nil, err
		}

		for relayerID, applicationRelayer := range applicationRelayersForSource {
			applicationRelayers[relayerID] = applicationRelayer
		}
		minHeights[sourceBlockchain.GetBlockchainID()] = minHeight

		logger.Info(
			"Created application relayers",
			zap.String("blockchainID", sourceBlockchain.BlockchainID),
		)
	}
	return applicationRelayers, minHeights, nil
}

// createApplicationRelayers creates Application Relayers for a given source blockchain.
func createApplicationRelayersForSourceChain(
	ctx context.Context,
	logger logging.Logger,
	metrics *relayer.ApplicationRelayerMetrics,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	sourceBlockchain config.SourceBlockchain,
	network *peers.AppRequestNetwork,
	messageCreator message.Creator,
	cfg *config.Config,
	currentHeight uint64,
	destinationClients map[ids.ID]vms.DestinationClient,
) (map[common.Hash]*relayer.ApplicationRelayer, uint64, error) {
	// Create the ApplicationRelayers
	logger.Info(
		"Creating application relayers",
		zap.String("originBlockchainID", sourceBlockchain.BlockchainID),
	)
	applicationRelayers := make(map[common.Hash]*relayer.ApplicationRelayer)

	// Each ApplicationRelayer determines its starting height based on the database state.
	// The Listener begins processing messages starting from the minimum height across all the ApplicationRelayers
	minHeight := uint64(0)
	for _, relayerID := range database.GetSourceBlockchainRelayerIDs(&sourceBlockchain) {
		height, err := database.CalculateStartingBlockHeight(
			logger,
			db,
			relayerID,
			sourceBlockchain.ProcessHistoricalBlocksFromHeight,
			currentHeight,
		)
		if err != nil {
			logger.Error(
				"Failed to calculate starting block height",
				zap.String("relayerID", relayerID.ID.String()),
				zap.Error(err),
			)
			return nil, 0, err
		}
		if minHeight == 0 || height < minHeight {
			minHeight = height
		}
		applicationRelayer, err := relayer.NewApplicationRelayer(
			logger,
			metrics,
			network,
			messageCreator,
			relayerID,
			db,
			ticker,
			destinationClients[relayerID.DestinationBlockchainID],
			sourceBlockchain,
			height,
			cfg,
		)
		if err != nil {
			logger.Error(
				"Failed to create application relayer",
				zap.String("relayerID", relayerID.ID.String()),
				zap.Error(err),
			)
			return nil, 0, err
		}
		applicationRelayers[relayerID.ID] = applicationRelayer
	}
	return applicationRelayers, minHeight, nil
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
