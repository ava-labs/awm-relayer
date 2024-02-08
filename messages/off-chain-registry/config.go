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
