package peers

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrFailedToCreateAppRequestNetworkMetrics = errors.New("failed to create app request network metrics")
)

type AppRequestNetworkMetrics struct {
	infoAPICallLatencyMS   prometheus.Histogram
	pChainAPICallLatencyMS prometheus.Histogram
	connects               prometheus.Counter
	disconnects            prometheus.Counter
}

func newAppRequestNetworkMetrics(registerer prometheus.Registerer) (*AppRequestNetworkMetrics, error) {
	infoAPICallLatencyMS := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "info_api_call_latency_ms",
			Help:    "Latency of calling info api in milliseconds",
			Buckets: prometheus.ExponentialBucketsRange(100, 10000, 10),
		},
	)
	if infoAPICallLatencyMS == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(infoAPICallLatencyMS)

	pChainAPICallLatencyMS := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "p_chain_api_call_latency_ms",
			Help:    "Latency of calling p-chain rpc in milliseconds",
			Buckets: prometheus.ExponentialBucketsRange(100, 10000, 10),
		},
	)
	if pChainAPICallLatencyMS == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(pChainAPICallLatencyMS)

	connects := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "connects",
			Help: "Number of connected events",
		},
	)
	if connects == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(connects)

	disconnects := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "disconnects",
			Help: "Number of disconnected events",
		},
	)
	if disconnects == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(disconnects)

	return &AppRequestNetworkMetrics{
		infoAPICallLatencyMS:   infoAPICallLatencyMS,
		pChainAPICallLatencyMS: pChainAPICallLatencyMS,
		connects:               connects,
		disconnects:            disconnects,
	}, nil
}
