// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type Config struct {
	RewardAddress string `json:"reward-address"`
}

func (c *Config) Validate() error {
	if !common.IsHexAddress(c.RewardAddress) {
		return fmt.Errorf("invalid reward address for EVM source subnet: %s", c.RewardAddress)
	}
	return nil
}
