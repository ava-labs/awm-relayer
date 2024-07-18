package peers

import (
	"errors"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrFailedToCreateAppRequestNetworkMetrics = errors.New("failed to create app request network metrics")
)

type AppRequestNetworkMetrics struct {
	infoAPIBaseURL   string
	pChainAPIBaseURL string

	infoAPICallLatencyMS   *prometheus.HistogramVec
	pChainAPICallLatencyMS *prometheus.HistogramVec
}

func NewAppRequestNetworkMetrics(cfg *config.Config, registerer prometheus.Registerer) (*AppRequestNetworkMetrics, error) {
	infoAPICallLatencyMS := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "info_api_call_latency_ms",
			Help:    "Latency of calling info api in milliseconds",
			Buckets: prometheus.LinearBuckets(10, 10, 10),
		},
		[]string{"info_api_base_url"},
	)
	if infoAPICallLatencyMS == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(infoAPICallLatencyMS)

	pChainAPICallLatencyMS := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "p_chain_api_call_latency_ms",
			Help:    "Latency of calling p-chain rpc in milliseconds",
			Buckets: prometheus.LinearBuckets(10, 10, 10),
		},
		[]string{"p_chain_api_base_url"},
	)
	if pChainAPICallLatencyMS == nil {
		return nil, ErrFailedToCreateAppRequestNetworkMetrics
	}
	registerer.MustRegister(pChainAPICallLatencyMS)

	return &AppRequestNetworkMetrics{
		infoAPIBaseURL:         cfg.InfoAPI.BaseURL,
		pChainAPIBaseURL:       cfg.PChainAPI.BaseURL,
		infoAPICallLatencyMS:   infoAPICallLatencyMS,
		pChainAPICallLatencyMS: pChainAPICallLatencyMS,
	}, nil
}
