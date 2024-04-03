// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchainregistry

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type Config struct {
	TeleporterRegistryAddress string `json:"teleporter-registry-address"`
}

func (c *Config) Validate() error {
	if !common.IsHexAddress(c.TeleporterRegistryAddress) {
		return fmt.Errorf("invalid address for TeleporterRegistry: %s", c.TeleporterRegistryAddress)
	}
	return nil
}
