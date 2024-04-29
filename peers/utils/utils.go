// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/awm-relayer/config"
)

// InitializeOptions initializes the rpc options for an API
func InitializeOptions(apiConfig *config.APIConfig) []rpc.Option {
	options := make([]rpc.Option, 0, len(apiConfig.QueryParams))
	for key, value := range apiConfig.QueryParams {
		options = append(options, rpc.WithQueryParam(key, value))
	}
	return options
}
