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
	AggregateSignaturesLatencyMS    prometheus.GaugeOpts
	AggregateSignaturesRequestCount prometheus.CounterOpts
	ValidatorFailures               prometheus.CounterOpts
}{
	AggregateSignaturesLatencyMS: prometheus.GaugeOpts{
		Name: "agg_sigs_latency_ms",
		Help: "Latency of requests for aggregate signatures",
	},
	AggregateSignaturesRequestCount: prometheus.CounterOpts{
		Name: "agg_sigs_req_count",
		Help: "Number of requests for aggregate signatures",
	},
	ValidatorFailures: prometheus.CounterOpts{
		Name: "validator_failures",
		Help: "Number of failed requests to validator nodes",
	},
}

type SignatureAggregatorMetrics struct {
	AggregateSignaturesLatencyMS    prometheus.Gauge
	AggregateSignaturesRequestCount prometheus.Counter
	ValidatorFailures               prometheus.Counter

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
		ValidatorFailures: prometheus.NewCounter(
			Opts.ValidatorFailures,
		),
	}

	registerer.MustRegister(m.AggregateSignaturesLatencyMS)
	registerer.MustRegister(m.AggregateSignaturesRequestCount)
	registerer.MustRegister(m.ValidatorFailures)

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
