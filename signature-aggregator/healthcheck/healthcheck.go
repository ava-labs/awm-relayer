package healthcheck

import (
	"context"
	"net/http"

	"github.com/alexliesenfeld/health"
)

func HandleHealthCheckRequest() {
	healthChecker := health.NewChecker(
		health.WithCheck(health.Check{
			Name:  "signature-aggregator",
			Check: func(context.Context) error { return nil },
		}),
	)

	http.Handle("/health", health.NewHandler(healthChecker))
}
