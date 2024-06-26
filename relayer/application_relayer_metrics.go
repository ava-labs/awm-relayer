// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrFailedToCreateApplicationRelayerMetrics = errors.New("failed to create application relayer metrics")
)

type ApplicationRelayerMetrics struct {
	successfulRelayMessageCount   *prometheus.CounterVec
	createSignedMessageLatencyMS  *prometheus.GaugeVec
	failedRelayMessageCount       *prometheus.CounterVec
	fetchSignatureAppRequestCount *prometheus.CounterVec
	fetchSignatureRPCCount        *prometheus.CounterVec
}

func NewApplicationRelayerMetrics(registerer prometheus.Registerer) (*ApplicationRelayerMetrics, error) {
	successfulRelayMessageCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "successful_relay_message_count",
			Help: "Number of messages that relayed successfully",
		},
		[]string{"destination_chain_id", "source_chain_id", "source_subnet_id"},
	)
	registerer.MustRegister(successfulRelayMessageCount)

	createSignedMessageLatencyMS := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "create_signed_message_latency_ms",
			Help: "Latency of creating a signed message in milliseconds",
		},
		[]string{"destination_chain_id", "source_chain_id", "source_subnet_id"},
	)
	if createSignedMessageLatencyMS == nil {
		return nil, ErrFailedToCreateApplicationRelayerMetrics
	}
	registerer.MustRegister(createSignedMessageLatencyMS)

	failedRelayMessageCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "failed_relay_message_count",
			Help: "Number of messages that failed to relay",
		},
		[]string{"destination_chain_id", "source_chain_id", "source_subnet_id", "failure_reason"},
	)
	if failedRelayMessageCount == nil {
		return nil, ErrFailedToCreateApplicationRelayerMetrics
	}
	registerer.MustRegister(failedRelayMessageCount)

	fetchSignatureAppRequestCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fetch_signature_app_request_count",
			Help: "Number messages signed via AppRequest",
		},
		[]string{"destination_chain_id", "source_chain_id", "source_subnet_id"},
	)
	if fetchSignatureAppRequestCount == nil {
		return nil, ErrFailedToCreateApplicationRelayerMetrics
	}
	registerer.MustRegister(fetchSignatureAppRequestCount)

	fetchSignatureRPCCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fetch_signature_rpc_count",
			Help: "Number messages signed via Warp API",
		},
		[]string{"destination_chain_id", "source_chain_id", "source_subnet_id"},
	)
	if fetchSignatureRPCCount == nil {
		return nil, ErrFailedToCreateApplicationRelayerMetrics
	}
	registerer.MustRegister(fetchSignatureRPCCount)

	return &ApplicationRelayerMetrics{
		successfulRelayMessageCount:   successfulRelayMessageCount,
		createSignedMessageLatencyMS:  createSignedMessageLatencyMS,
		failedRelayMessageCount:       failedRelayMessageCount,
		fetchSignatureAppRequestCount: fetchSignatureAppRequestCount,
		fetchSignatureRPCCount:        fetchSignatureRPCCount,
	}, nil
}
