// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Teleporter configuration.
// If FeeAssets is empty, than the relayer will relay all messages, regardless of fee asset.
type Config struct {
	RewardAddress string   `json:"reward-address"`
	FeeAssets     []string `json:"fee-assets"`
}

func (c *Config) Validate() error {
	if !common.IsHexAddress(c.RewardAddress) {
		return fmt.Errorf("invalid reward address for EVM source subnet: %s", c.RewardAddress)
	}
	for _, asset := range c.FeeAssets {
		if !common.IsHexAddress(asset) {
			return fmt.Errorf("invalid fee asset address for EVM source subnet: %s", asset)
		}
	}
	return nil
}
