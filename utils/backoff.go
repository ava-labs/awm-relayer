package utils

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// WithMaxRetriesLog runs the operation until it succeeds or max retries has been reached.
// It uses exponential back off.
// It optionally logs information if logger is set.
func WithMaxRetriesLog(
	operation backoff.Operation,
	max uint64,
	logger logging.Logger,
	msg string,
	fields ...zapcore.Field,
) error {
	attempt := uint(1)
	expBackOff := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), max)
	notify := func(err error, duration time.Duration) {
		if logger == nil {
			return
		}
		fields := append(fields, zap.Uint("attempt", attempt), zap.Error(err), zap.Duration("backoff", duration))
		logger.Warn(msg, fields...)
		attempt++
	}
	err := backoff.RetryNotify(operation, expBackOff, notify)
	if err != nil && logger != nil {
		fields := append(fields, zap.Uint64("attempts", uint64(attempt)), zap.Error(err))
		logger.Error(msg, fields...)
	}
	return err
}

// WithMaxRetries rens the operation until it succeeds or max retries has been reached.
// It uses exponential back off.
func WithMaxRetries(operation backoff.Operation, max uint64) error {
	return WithMaxRetriesLog(operation, max, nil, "")
}
