// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/ids"
	baseCfg "github.com/ava-labs/awm-relayer/config"
)

type SourceBlockchain struct {
	SubnetID     string            `mapstructure:"subnet-id" json:"subnet-id"`
	BlockchainID string            `mapstructure:"blockchain-id" json:"blockchain-id"` //nolint:lll
	VM           string            `mapstructure:"vm" json:"vm"`
	RPCEndpoint  baseCfg.APIConfig `mapstructure:"rpc-endpoint" json:"rpc-endpoint"` //nolint:lll
	WSEndpoint   baseCfg.APIConfig `mapstructure:"ws-endpoint" json:"ws-endpoint"`   //nolint:lll

	// convenience fields to access parsed data after initialization
	subnetID     ids.ID
	blockchainID ids.ID
}

func (s *SourceBlockchain) GetSubnetID() ids.ID {
	return s.subnetID
}

func (s *SourceBlockchain) GetBlockchainID() ids.ID {
	return s.blockchainID
}
