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
	"sync"
	"sync/atomic"

	"github.com/alexliesenfeld/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/vms"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	defaultErrorChanSize = 1000
	defaultApiPort       = 8080
	defaultMetricsPort   = 9090
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
	sourceSubnetIDs, sourceChainIDs, err := cfg.GetSourceIDs()
	if err != nil {
		logger.Error(
			"Failed to get source IDs",
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

	network, responseChans, err := peers.NewNetwork(logger, registerer, cfg.NetworkID, sourceSubnetIDs, sourceChainIDs, cfg.PChainAPIURL)
	if err != nil {
		logger.Error(
			"Failed to create app request network",
			zap.Error(err),
		)
		return
	}

	// Create a health check server that polls a single atomic bool, settable by any relayer goroutine on failure
	healthy := atomic.Bool{}
	healthy.Store(true)

	checker := health.NewChecker(
		health.WithCheck(health.Check{
			Name: "relayers-all",
			Check: func(context.Context) error {
				if !healthy.Load() {
					return fmt.Errorf("relayer is unhealthy")
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

	// Create relayers for each of the subnets configured as a source
	var wg sync.WaitGroup
	for _, s := range cfg.SourceSubnets {
		chainID, err := ids.FromString(s.ChainID)
		if err != nil {
			logger.Error(
				"Invalid subnetID in configuration",
				zap.Error(err),
			)
			return
		}
		wg.Add(1)
		subnetInfo := s
		go func() {
			defer func() {
				wg.Done()
				healthy.Store(false)
			}()
			runRelayer(logger, metrics, subnetInfo, pChainClient, network, responseChans[chainID], destinationClients, messageCreator)
			logger.Info(
				"Relayer exiting.",
				zap.String("chainID", chainID.String()),
			)
		}()
	}
	wg.Wait()
}

// runRelayer creates a relayer instance for a subnet. It listens for warp messages on that subnet, and handles delivery to the destination
func runRelayer(logger logging.Logger,
	metrics *relayer.MessageRelayerMetrics,
	sourceSubnetInfo config.SourceSubnet,
	pChainClient platformvm.Client,
	network *peers.AppRequestNetwork,
	responseChan chan message.InboundMessage,
	destinationClients map[ids.ID]vms.DestinationClient,
	messageCreator message.Creator,
) {
	logger.Info(
		"Creating relayer",
		zap.String("chainID", sourceSubnetInfo.ChainID),
	)
	errorChan := make(chan error, defaultErrorChanSize)

	relayer, subscriber, err := relayer.NewRelayer(
		logger,
		sourceSubnetInfo,
		errorChan,
		pChainClient,
		network,
		responseChan,
		destinationClients,
	)
	if err != nil {
		logger.Error(
			"Failed to create relayer instance",
			zap.Error(err),
		)
		return
	}
	logger.Info(
		"Created relayer. Listening for messages to relay.",
		zap.String("chainID", sourceSubnetInfo.ChainID),
	)

	// Wait for logs from the subscribed node
	for {
		select {
		case txLog := <-subscriber.Logs():
			logger.Info(
				"Handling Teleporter submit message log.",
				zap.String("txId", hex.EncodeToString(txLog.SourceTxID)),
				zap.String("originChainId", sourceSubnetInfo.ChainID),
				zap.String("destinationChainId", txLog.DestinationChainID.String()),
				zap.String("destinationChainAddress", txLog.DestinationAddress.String()),
				zap.String("sourceAddress", txLog.SourceAddress.String()),
			)

			// Relay the message to the destination chain. Continue on failure.
			err = relayer.RelayMessage(&txLog, metrics, messageCreator)
			if err != nil {
				logger.Error(
					"Error relaying message",
					zap.String("originChainID", sourceSubnetInfo.ChainID),
					zap.Error(err),
				)
				continue
			}
		case err := <-subscriber.Err():
			logger.Error(
				"Received error from subscribed node",
				zap.String("originChainID", sourceSubnetInfo.ChainID),
				zap.Error(err),
			)
			err = subscriber.Subscribe()
			if err != nil {
				logger.Error(
					"Failed to resubscribe to node. Relayer goroutine exiting.",
					zap.String("originChainID", sourceSubnetInfo.ChainID),
					zap.Error(err),
				)
				return
			}
		case err := <-errorChan:
			logger.Error(
				"Relayer goroutine stopped with error. Relayer goroutine exiting.",
				zap.String("originChainID", sourceSubnetInfo.ChainID),
				zap.Error(err),
			)
			return
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
