package healthcheck

import (
	"context"
	"fmt"
	"net/http"

	"github.com/alexliesenfeld/health"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

func Run(
	logger logging.Logger,
	port uint16,
	checkFunc func(context.Context) error,
) {
	healthChecker := health.NewChecker(
		health.WithCheck(health.Check{
			Name:  "signature-aggregator",
			Check: checkFunc,
		}),
	)

	http.Handle("/health", health.NewHandler(healthChecker))

	go func() {
		logger.Info(
			"Listening for health check requests",
			zap.Uint16("port", port),
		)
		err := http.ListenAndServe(
			fmt.Sprintf(":%d", port),
			nil,
		)
		if err != nil {
			logger.Fatal(
				"failed to listen and serve HTTP",
				zap.Error(err),
			)
		}
	}()
}
