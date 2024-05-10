// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethclient

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddQueryParams(t *testing.T) {
	t.Run("NoQueryParams", func(t *testing.T) {
		newurl, err := addQueryParams("https://avalabs.com", nil)
		require.NoError(t, err)
		require.Equal(t, "https://avalabs.com", newurl)
	})
	t.Run("TwoQueryParams", func(t *testing.T) {
		newurl, err := addQueryParams("https://avalabs.com", map[string]string{
			"first":  "value1",
			"second": "value2",
		})
		require.NoError(t, err)
		require.Equal(t, "https://avalabs.com?first=value1&second=value2", newurl)
	})
	t.Run("InvalidEndpoint", func(t *testing.T) {
		_, err := addQueryParams("invalid-endpoint", nil)
		require.True(t, errors.Is(err, ErrInvalidEndpoint))
	})
}

func TestNewClientOptions(t *testing.T) {
	t.Run("NoHttpHeaders", func(t *testing.T) {
		opts := newClientHeaderOptions(nil)
		require.Len(t, opts, 0)
	})
	t.Run("TwoHttpHeaders", func(t *testing.T) {
		opts := newClientHeaderOptions(map[string]string{
			"first":  "value1",
			"second": "value2",
		})
		require.Len(t, opts, 2)
	})
}
