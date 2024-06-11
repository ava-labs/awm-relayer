// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/rpc"
)

var ErrInvalidEndpoint = errors.New("invalid rpc endpoint")

// DialWithContext returns an ethclient.Client with the internal RPC client configured with the provided options.
func DialWithConfig(ctx context.Context, baseURL string, httpHeaders, queryParams map[string]string) (ethclient.Client, error) {
	url, err := addQueryParams(baseURL, queryParams)
	if err != nil {
		return nil, err
	}
	client, err := rpc.DialOptions(ctx, url, newClientHeaderOptions(httpHeaders)...)
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(client), nil
}

// addQueryParams adds the query parameters to the url
func addQueryParams(endpoint string, queryParams map[string]string) (string, error) {
	uri, err := url.ParseRequestURI(endpoint)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	values := uri.Query()
	for key, value := range queryParams {
		values.Add(key, value)
	}
	uri.RawQuery = values.Encode()
	return uri.String(), nil
}

// newClientOptions creates a ClientOption slice from httpHeaders
func newClientHeaderOptions(httpHeaders map[string]string) []rpc.ClientOption {
	opts := make([]rpc.ClientOption, 0, len(httpHeaders))
	for key, value := range httpHeaders {
		opts = append(opts, rpc.WithHeader(key, value))
	}
	return opts
}
