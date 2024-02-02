// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrFailedToCreateMessageRelayerMetrics = errors.New("failed to create message relayer metrics")
)

type MessageRelayerMetrics struct {
	successfulRelayMessageCount  *prometheus.CounterVec
	createSignedMessageLatencyMS *prometheus.GaugeVec
	failedRelayMessageCount      *prometheus.CounterVec
}

func NewMessageRelayerMetrics(registerer prometheus.Registerer) (*MessageRelayerMetrics, error) {
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
		return nil, ErrFailedToCreateMessageRelayerMetrics
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
		return nil, ErrFailedToCreateMessageRelayerMetrics
	}
	registerer.MustRegister(failedRelayMessageCount)

	return &MessageRelayerMetrics{
		successfulRelayMessageCount:  successfulRelayMessageCount,
		createSignedMessageLatencyMS: createSignedMessageLatencyMS,
		failedRelayMessageCount:      failedRelayMessageCount,
	}, nil
}
