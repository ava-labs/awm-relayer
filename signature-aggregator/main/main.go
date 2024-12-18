// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/icm-services/peers"
	"github.com/ava-labs/icm-services/signature-aggregator/aggregator"
	"github.com/ava-labs/icm-services/signature-aggregator/api"
	"github.com/ava-labs/icm-services/signature-aggregator/config"
	"github.com/ava-labs/icm-services/signature-aggregator/healthcheck"
	"github.com/ava-labs/icm-services/signature-aggregator/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var version = "v0.0.0-dev"

func main() {
	fs := config.BuildFlagSet()
	if err := fs.Parse(os.Args[1:]); err != nil {
		config.DisplayUsageText()
		panic(fmt.Errorf("Failed to parse flags: %w", err))
	}

	displayVersion, err := fs.GetBool(config.VersionKey)
	if err != nil {
		panic(fmt.Errorf("error reading %s flag: %w", config.VersionKey, err))
	}
	if displayVersion {
		fmt.Printf("%s\n", version)
		os.Exit(0)
	}

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

	logLevel, err := logging.ToLevel(cfg.LogLevel)
	if err != nil {
		panic(fmt.Errorf("error with log level: %w", err))
	}

	logger := logging.NewLogger(
		"signature-aggregator",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	logger.Info("Initializing signature-aggregator")

	// Initialize the global app request network
	logger.Info("Initializing app request network")
	// The app request network generates P2P networking logs that are verbose at the info level.
	// Unless the log level is debug or lower, set the network log level to error to avoid spamming the logs.
	// We do not collect metrics for the network.
	networkLogLevel := logging.Error
	if logLevel <= logging.Debug {
		networkLogLevel = logLevel
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

	network, err := peers.NewNetwork(
		networkLogLevel,
		prometheus.DefaultRegisterer,
		nil,
		nil,
		&cfg,
	)
	if err != nil {
		logger.Fatal("Failed to create app request network", zap.Error(err))
		panic(err)
	}
	defer network.Shutdown()

	registry := metrics.Initialize(cfg.MetricsPort)
	metricsInstance := metrics.NewSignatureAggregatorMetrics(registry)

	signatureAggregator, err := aggregator.NewSignatureAggregator(
		network,
		logger,
		messageCreator,
		cfg.SignatureCacheSize,
		metricsInstance,
	)
	if err != nil {
		logger.Fatal("Failed to create signature aggregator", zap.Error(err))
		panic(err)
	}

	api.HandleAggregateSignaturesByRawMsgRequest(
		logger,
		metricsInstance,
		signatureAggregator,
	)
	healthcheck.HandleHealthCheckRequest()

	logger.Info("Initialization complete")
	err = http.ListenAndServe(fmt.Sprintf(":%d", cfg.APIPort), nil)
	if errors.Is(err, http.ErrServerClosed) {
		logger.Info("server closed")
	} else if err != nil {
		logger.Error("server error", zap.Error(err))
		log.Fatal(err)
	}
}
