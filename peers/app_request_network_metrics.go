package peers

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrFailedToCreateAppRequestNetworkMetrics = errors.New("failed to create app request network metrics")
)

type AppRequestNetworkMetrics struct {
	pChainAPICallLatencyMS prometheus.Histogram
	connects               prometheus.Counter
	disconnects            prometheus.Counter
}

func newAppRequestNetworkMetrics(registerer prometheus.Registerer) (*AppRequestNetworkMetrics, error) {
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
		pChainAPICallLatencyMS: pChainAPICallLatencyMS,
		connects:               connects,
		disconnects:            disconnects,
	}, nil
}
