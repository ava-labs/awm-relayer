// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/awm-relayer/config"
)

// PChainAPI holds a platformvm.Client and options for querying the P-Chain
type PChainAPI struct {
	client  platformvm.Client
	options []rpc.Option
}

// InfoAPI holds an info.Client and options for querying the Info API
type InfoAPI struct {
	client  info.Client
	options []rpc.Option
}

func NewPChainAPI(apiConfig *config.APIConfig) (*PChainAPI, error) {
	client := platformvm.NewClient(apiConfig.BaseURL)
	options := make([]rpc.Option, 0, len(apiConfig.QueryParams))
	for key, value := range apiConfig.QueryParams {
		options = append(options, rpc.WithQueryParam(key, value))
	}
	return &PChainAPI{
		client:  client,
		options: options,
	}, nil
}

func NewInfoAPI(apiConfig *config.APIConfig) (*InfoAPI, error) {
	client := info.NewClient(apiConfig.BaseURL)
	options := make([]rpc.Option, 0, len(apiConfig.QueryParams))
	for key, value := range apiConfig.QueryParams {
		options = append(options, rpc.WithQueryParam(key, value))
	}
	return &InfoAPI{
		client:  client,
		options: options,
	}, nil
}
