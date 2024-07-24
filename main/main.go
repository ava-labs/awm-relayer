// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/api"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	logger.Info("Initializing source clients")
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
	// We do not collect metrics for the network.
	networkLogLevel := logging.Error
	if logLevel <= logging.Debug {
		networkLogLevel = logLevel
	}
	network, err := peers.NewNetwork(
		networkLogLevel,
		prometheus.DefaultRegisterer,
		&cfg,
	)
	if err != nil {
		logger.Fatal("Failed to create app request network", zap.Error(err))
		panic(err)
	}

	startMetricsServer(logger, gatherer, cfg.MetricsPort)

	relayerMetrics, err := relayer.NewApplicationRelayerMetrics(registerer)
	if err != nil {
		logger.Fatal("Failed to create application relayer metrics", zap.Error(err))
		panic(err)
	}

	// Initialize message creator passed down to relayers for creating app requests.
	// We do not collect metrics for the message creator.
	messageCreator, err := message.NewCreator(
		logger,
		prometheus.DefaultRegisterer,
		constants.DefaultNetworkCompressionType,
		constants.DefaultNetworkMaximumInboundTimeout,
	)
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

	relayerHealth := createHealthTrackers(&cfg)

	deciderClient, err := createDeciderClient(
		cfg.DeciderHost,
		cfg.DeciderPort,
	)
	if err != nil {
		logger.Fatal(
			"Failed to instantiate decider client",
			zap.Error(err),
		)
		panic(err)
	}

	messageHandlerFactories, err := createMessageHandlerFactories(
		logger,
		&cfg,
		deciderClient,
	)
	if err != nil {
		logger.Fatal("Failed to create message handler factories", zap.Error(err))
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
		logger.Fatal("Failed to create application relayers", zap.Error(err))
		panic(err)
	}
	messageCoordinator := relayer.NewMessageCoordinator(
		logger,
		messageHandlerFactories,
		applicationRelayers,
		sourceClients,
	)

	// Each Listener goroutine will have an atomic bool that it can set to false to indicate an unrecoverable error
	api.HandleHealthCheck(logger, relayerHealth)
	api.HandleRelay(logger, messageCoordinator)
	api.HandleRelayMessage(logger, messageCoordinator)

	// start the health check server
	go func() {
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", cfg.APIPort), nil))
	}()

	// Create listeners for each of the subnets configured as a source
	errGroup, ctx := errgroup.WithContext(context.Background())
	for _, s := range cfg.SourceBlockchains {
		sourceBlockchain := s

		// errgroup will cancel the context when the first goroutine returns an error
		errGroup.Go(func() error {
			// runListener runs until it errors or the context is canceled by another goroutine
			return relayer.RunListener(
				ctx,
				logger,
				*sourceBlockchain,
				sourceClients[sourceBlockchain.GetBlockchainID()],
				relayerHealth[sourceBlockchain.GetBlockchainID()],
				cfg.ProcessMissedBlocks,
				minHeights[sourceBlockchain.GetBlockchainID()],
				messageCoordinator,
			)
		})
	}
	err = errGroup.Wait()
	logger.Error("Relayer exiting.", zap.Error(err))
}

func createMessageHandlerFactories(
	logger logging.Logger,
	globalConfig *config.Config,
	deciderClient *grpc.ClientConn,
) (map[ids.ID]map[common.Address]messages.MessageHandlerFactory, error) {
	messageHandlerFactories := make(map[ids.ID]map[common.Address]messages.MessageHandlerFactory)
	for _, sourceBlockchain := range globalConfig.SourceBlockchains {
		messageHandlerFactoriesForSource := make(map[common.Address]messages.MessageHandlerFactory)
		// Create message handler factories for each supported message protocol
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
					deciderClient,
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
				logger.Error("Failed to create message handler factory", zap.Error(err))
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
		clients[sourceBlockchain.GetBlockchainID()], err = utils.NewEthClientWithConfig(
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

/* if port is nil, neither a client nor an error will be returned.
 * if is non-nil, a client will be constructed
 * if host is an empty string, a default value of "localhost" is assumed. */
func createDeciderClient(host string, port *uint16) (*grpc.ClientConn, error) {
	if port == nil {
		return nil, nil
	}

	if len(host) == 0 {
		host = "localhost"
	}

	client, err := grpc.NewClient(
		strings.Join(
			[]string{host, strconv.FormatUint(uint64(*port), 10)},
			":",
		),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"Failed to instantiate grpc client: %w",
			err,
		)
	}

	runtime.SetFinalizer(
		client,
		func(c *grpc.ClientConn) { c.Close() },
	)

	return client, nil
}

func createHealthTrackers(cfg *config.Config) map[ids.ID]*atomic.Bool {
	healthTrackers := make(map[ids.ID]*atomic.Bool, len(cfg.SourceBlockchains))
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		healthTrackers[sourceBlockchain.GetBlockchainID()] = atomic.NewBool(true)
	}
	return healthTrackers
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
