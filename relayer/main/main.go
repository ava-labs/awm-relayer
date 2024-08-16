// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/relayer/api"
	"github.com/ava-labs/awm-relayer/relayer/checkpoint"
	"github.com/ava-labs/awm-relayer/relayer/config"
	"github.com/ava-labs/awm-relayer/signature-aggregator/aggregator"
	sigAggMetrics "github.com/ava-labs/awm-relayer/signature-aggregator/metrics"
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
	var trackedSubnets set.Set[ids.ID]
	// trackedSubnets is no longer strictly required but keeping it here for now
	// to keep full parity with existing AWM relayer for now
	// TODO: remove this from here once trackedSubnets are no longer referenced
	// by ping messages in avalanchego
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		trackedSubnets.Add(sourceBlockchain.GetSubnetID())
	}

	network, err := peers.NewNetwork(
		networkLogLevel,
		prometheus.DefaultRegisterer,
		trackedSubnets,
		&cfg,
	)
	initializeConnectionsAndCheckStake(logger, network, &cfg)

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

	deciderConnection, err := createDeciderConnection(cfg.DeciderURL)
	if err != nil {
		logger.Fatal(
			"Failed to instantiate decider connection",
			zap.Error(err),
		)
		panic(err)
	}

	messageHandlerFactories, err := createMessageHandlerFactories(
		logger,
		&cfg,
		deciderConnection,
	)
	if err != nil {
		logger.Fatal("Failed to create message handler factories", zap.Error(err))
		panic(err)
	}

	signatureAggregator := aggregator.NewSignatureAggregator(
		network,
		logger,
		sigAggMetrics.NewSignatureAggregatorMetrics(
			prometheus.DefaultRegisterer,
		),
		messageCreator,
	)

	applicationRelayers, minHeights, err := createApplicationRelayers(
		context.Background(),
		logger,
		relayerMetrics,
		db,
		ticker,
		network,
		&cfg,
		sourceClients,
		destinationClients,
		signatureAggregator,
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
	logger.Info("Initialization complete")
	err = errGroup.Wait()
	logger.Error("Relayer exiting.", zap.Error(err))
}

func createMessageHandlerFactories(
	logger logging.Logger,
	globalConfig *config.Config,
	deciderConnection *grpc.ClientConn,
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
					deciderConnection,
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
	cfg *config.Config,
	sourceClients map[ids.ID]ethclient.Client,
	destinationClients map[ids.ID]vms.DestinationClient,
	signatureAggregator *aggregator.SignatureAggregator,
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
			cfg,
			currentHeight,
			destinationClients,
			signatureAggregator,
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
	cfg *config.Config,
	currentHeight uint64,
	destinationClients map[ids.ID]vms.DestinationClient,
	signatureAggregator *aggregator.SignatureAggregator,
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

		checkpointManager := checkpoint.NewCheckpointManager(
			logger,
			db,
			ticker.Subscribe(),
			relayerID,
			height,
		)

		applicationRelayer, err := relayer.NewApplicationRelayer(
			logger,
			metrics,
			network,
			relayerID,
			destinationClients[relayerID.DestinationBlockchainID],
			sourceBlockchain,
			checkpointManager,
			cfg,
			signatureAggregator,
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

// create a connection to the "should send message" decider service.
// if url is unspecified, returns a nil client pointer
func createDeciderConnection(url string) (*grpc.ClientConn, error) {
	if len(url) == 0 {
		return nil, nil
	}

	connection, err := grpc.NewClient(
		url,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"Failed to instantiate grpc client: %w",
			err,
		)
	}

	runtime.SetFinalizer(
		connection,
		func(c *grpc.ClientConn) { c.Close() },
	)

	return connection, nil
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

func initializeConnectionsAndCheckStake(
	logger logging.Logger,
	network *peers.AppRequestNetwork,
	cfg *config.Config,
) error {
	// Manually connect to the validators of each of the source subnets.
	// We return an error if we are unable to connect to sufficient stake on any of the subnets.
	// Sufficient stake is determined by the Warp quora of the configured supported destinations,
	// or if the subnet supports all destinations, by the quora of all configured destinations.
	for _, sourceBlockchain := range cfg.SourceBlockchains {
		if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
			if err := connectToPrimaryNetworkPeers(logger, network, cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"failed to connect to primary network peers: %w",
					err,
				)
			}
		} else {
			if err := connectToNonPrimaryNetworkPeers(logger, network, cfg, sourceBlockchain); err != nil {
				return fmt.Errorf(
					"failed to connect to non-primary network peers: %w",
					err,
				)
			}
		}
	}
	return nil
}

// Connect to the validators of the source blockchain. For each destination blockchain,
// verify that we have connected to a threshold of stake.
func connectToNonPrimaryNetworkPeers(
	logger logging.Logger,
	network *peers.AppRequestNetwork,
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	subnetID := sourceBlockchain.GetSubnetID()
	connectedValidators, err := network.ConnectToCanonicalValidators(subnetID)
	if err != nil {
		logger.Error(
			"Failed to connect to canonical validators",
			zap.String("subnetID", subnetID.String()),
			zap.Error(err),
		)
		return err
	}
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		if ok, quorum, err := checkForSufficientConnectedStake(logger, cfg, connectedValidators, blockchainID); !ok {
			logger.Error(
				"Failed to connect to a threshold of stake",
				zap.String("destinationBlockchainID", blockchainID.String()),
				zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
				zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
				zap.Any("warpQuorum", quorum),
			)
			return err
		}
	}
	return nil
}

// Connect to the validators of the destination blockchains. Verify that we have connected
// to a threshold of stake for each blockchain.
func connectToPrimaryNetworkPeers(
	logger logging.Logger,
	network *peers.AppRequestNetwork,
	cfg *config.Config,
	sourceBlockchain *config.SourceBlockchain,
) error {
	for _, destination := range sourceBlockchain.SupportedDestinations {
		blockchainID := destination.GetBlockchainID()
		subnetID := cfg.GetSubnetID(blockchainID)
		connectedValidators, err := network.ConnectToCanonicalValidators(subnetID)
		if err != nil {
			logger.Error(
				"Failed to connect to canonical validators",
				zap.String("subnetID", subnetID.String()),
				zap.Error(err),
			)
			return err
		}

		if ok, quorum, err := checkForSufficientConnectedStake(logger, cfg, connectedValidators, blockchainID); !ok {
			logger.Error(
				"Failed to connect to a threshold of stake",
				zap.String("destinationBlockchainID", blockchainID.String()),
				zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
				zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
				zap.Any("warpQuorum", quorum),
			)
			return err
		}
	}
	return nil
}

// Fetch the warp quorum from the config and check if the connected stake exceeds the threshold
func checkForSufficientConnectedStake(
	logger logging.Logger,
	cfg *config.Config,
	connectedValidators *peers.ConnectedCanonicalValidators,
	destinationBlockchainID ids.ID,
) (bool, *config.WarpQuorum, error) {
	quorum, err := cfg.GetWarpQuorum(destinationBlockchainID)
	if err != nil {
		logger.Error(
			"Failed to get warp quorum from config",
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
			zap.Error(err),
		)
		return false, nil, err
	}
	return utils.CheckStakeWeightExceedsThreshold(
		big.NewInt(0).SetUint64(connectedValidators.ConnectedWeight),
		connectedValidators.TotalValidatorWeight,
		quorum.QuorumNumerator,
		quorum.QuorumDenominator,
	), &quorum, nil
}
