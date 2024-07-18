// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	baseCfg "github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
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

func (s *SourceBlockchain) Validate() error {
	if err := s.RPCEndpoint.Validate(); err != nil {
		return fmt.Errorf("invalid rpc-endpoint in source subnet configuration: %w", err)
	}
	if err := s.WSEndpoint.Validate(); err != nil {
		return fmt.Errorf("invalid ws-endpoint in source subnet configuration: %w", err)
	}
	blockchainID, err := utils.HexOrCB58ToID(s.BlockchainID)
	if err != nil {
		return fmt.Errorf("invalid blockchainID '%s' in configuration. error: %w", s.BlockchainID, err)
	}
	s.blockchainID = blockchainID

	subnetID, err := utils.HexOrCB58ToID(s.SubnetID)
	if err != nil {
		return fmt.Errorf("invalid subnetID '%s' in configuration. error: %w", s.SubnetID, err)
	}
	s.subnetID = subnetID

	return nil
}

func (s *SourceBlockchain) GetSubnetID() ids.ID {
	return s.subnetID
}

func (s *SourceBlockchain) GetBlockchainID() ids.ID {
	return s.blockchainID
}

func FromBaseSourceBlockChain(baseSourceBlockchain *baseCfg.SourceBlockchain) (*SourceBlockchain, error) {
	sourceBlockchain := SourceBlockchain{
		SubnetID:     baseSourceBlockchain.SubnetID,
		BlockchainID: baseSourceBlockchain.BlockchainID,
		VM:           baseSourceBlockchain.VM,
		RPCEndpoint:  baseSourceBlockchain.RPCEndpoint,
		WSEndpoint:   baseSourceBlockchain.WSEndpoint,
	}
	if err := sourceBlockchain.Validate(); err != nil {
		return nil, err
	}
	return &sourceBlockchain, nil
}
