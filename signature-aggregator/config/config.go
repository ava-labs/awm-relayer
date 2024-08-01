// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
	baseCfg "github.com/ava-labs/awm-relayer/config"
)

const (
	defaultAPIPort = uint16(8080)
)

var defaultLogLevel = logging.Info.String()

const usageText = `
Usage:
signature-aggregator --config-file path-to-config            Specifies the config file and start the signing service.
signature-aggregator --version                               Display signature-aggregator version and exit.
signature-aggregator --help                                  Display signature-aggregator usage and exit.
`

type Config struct {
	LogLevel  string             `mapstructure:"log-level" json:"log-level"`
	PChainAPI *baseCfg.APIConfig `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI   *baseCfg.APIConfig `mapstructure:"info-api" json:"info-api"`
	APIPort   uint16             `mapstructure:"api-port" json:"api-port"`

	MetricsPort uint16 `mapstructure:"metrics-port" json:"metrics-port"`
}

func DisplayUsageText() {
	fmt.Printf("%s\n", usageText)
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters.
func (c *Config) Validate() error {
	if err := c.PChainAPI.Validate(); err != nil {
		return err
	}
	if err := c.InfoAPI.Validate(); err != nil {
		return err
	}

	return nil
}

// Config implempents the peers.Config interface
func (c *Config) GetPChainAPI() *baseCfg.APIConfig {
	return c.PChainAPI
}

// Config implempents the peers.Config interface
func (c *Config) GetInfoAPI() *baseCfg.APIConfig {
	return c.InfoAPI
}
