// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ErrFailedToCreateSignatureAggregatorMetrics = errors.New(
		"failed to create signature aggregator metrics",
	)
)

var Opts = struct {
	AggregateSignaturesLatencyMS       prometheus.GaugeOpts
	AggregateSignaturesRequestCount    prometheus.CounterOpts
	FailuresToGetValidatorSet          prometheus.CounterOpts
	FailuresToConnectToSufficientStake prometheus.CounterOpts
	FailuresSendingToNode              prometheus.CounterOpts
	ValidatorTimeouts                  prometheus.CounterOpts
	InvalidSignatureResponses          prometheus.CounterOpts
}{
	AggregateSignaturesLatencyMS: prometheus.GaugeOpts{
		Name: "agg_sigs_latency_ms",
		Help: "Latency of requests for aggregate signatures",
	},
	AggregateSignaturesRequestCount: prometheus.CounterOpts{
		Name: "agg_sigs_req_count",
		Help: "Number of requests for aggregate signatures",
	},
	FailuresToGetValidatorSet: prometheus.CounterOpts{
		Name: "failures_to_get_validator_set",
		Help: "Number of failed attempts to retrieve the validator set",
	},
	FailuresToConnectToSufficientStake: prometheus.CounterOpts{
		Name: "failures_to_connect_to_sufficient_stake",
		Help: "Number of incidents of connecting to some validators but not enough stake weight",
	},
	FailuresSendingToNode: prometheus.CounterOpts{
		Name: "failures_sending_to_node",
		Help: "Number of failures to send a request to a validator node",
	},
	ValidatorTimeouts: prometheus.CounterOpts{
		Name: "validator_timeouts",
		Help: "Number of timeouts while waiting for a validator to respond to a request",
	},
	InvalidSignatureResponses: prometheus.CounterOpts{
		Name: "invalid_signature_responses",
		Help: "Number of responses from validators that were not valid signatures",
	},
}

type SignatureAggregatorMetrics struct {
	AggregateSignaturesLatencyMS       prometheus.Gauge
	AggregateSignaturesRequestCount    prometheus.Counter
	FailuresToGetValidatorSet          prometheus.Counter
	FailuresToConnectToSufficientStake prometheus.Counter
	FailuresSendingToNode              prometheus.Counter
	ValidatorTimeouts                  prometheus.Counter
	InvalidSignatureResponses          prometheus.Counter

	// TODO: consider other failures to monitor. Issue #384 requires
	// "network failures", but we probably don't handle those directly.
	// Surely there are some error types specific to this layer that we can
	// count.

	// TODO: consider how the relayer keeps separate counts of aggregations
	// by AppRequest vs by Warp API and whether we should have such counts.
}

func NewSignatureAggregatorMetrics(
	registerer prometheus.Registerer,
) *SignatureAggregatorMetrics {
	m := SignatureAggregatorMetrics{
		AggregateSignaturesLatencyMS: prometheus.NewGauge(
			Opts.AggregateSignaturesLatencyMS,
		),
		AggregateSignaturesRequestCount: prometheus.NewCounter(
			Opts.AggregateSignaturesRequestCount,
		),
		FailuresToGetValidatorSet: prometheus.NewCounter(
			Opts.FailuresToGetValidatorSet,
		),
		FailuresToConnectToSufficientStake: prometheus.NewCounter(
			Opts.FailuresToConnectToSufficientStake,
		),
		FailuresSendingToNode: prometheus.NewCounter(
			Opts.FailuresSendingToNode,
		),
		ValidatorTimeouts: prometheus.NewCounter(
			Opts.ValidatorTimeouts,
		),
		InvalidSignatureResponses: prometheus.NewCounter(
			Opts.InvalidSignatureResponses,
		),
	}

	registerer.MustRegister(m.AggregateSignaturesLatencyMS)
	registerer.MustRegister(m.AggregateSignaturesRequestCount)
	registerer.MustRegister(m.FailuresToGetValidatorSet)
	registerer.MustRegister(m.FailuresToConnectToSufficientStake)
	registerer.MustRegister(m.FailuresSendingToNode)
	registerer.MustRegister(m.ValidatorTimeouts)
	registerer.MustRegister(m.InvalidSignatureResponses)

	return &m
}

func (m *SignatureAggregatorMetrics) HandleMetricsRequest(
	gatherer metrics.MultiGatherer,
) {
	http.Handle(
		"/metrics",
		promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}),
	)
}

func Initialize(port uint16) *prometheus.Registry {
	gatherer := metrics.NewPrefixGatherer()
	registry := prometheus.NewRegistry()
	err := gatherer.Register("signature-aggregator", registry)
	if err != nil {
		panic(
			fmt.Errorf(
				"failed to register metrics gatherer: %w",
				err,
			),
		)
	}

	http.Handle(
		"/metrics",
		promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}),
	)

	go func() {
		log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()

	return registry
}
