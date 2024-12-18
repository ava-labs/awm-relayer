// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
	basecfg "github.com/ava-labs/icm-services/config"
	"github.com/ava-labs/icm-services/peers"
)

const (
	defaultAPIPort     = uint16(8080)
	defaultMetricsPort = uint16(8081)

	DefaultSignatureCacheSize = uint64(1024 * 1024)
)

var defaultLogLevel = logging.Info.String()

const usageText = `
Usage:
signature-aggregator --config-file path-to-config            Specifies the config file and start the signing service.
signature-aggregator --version                               Display signature-aggregator version and exit.
signature-aggregator --help                                  Display signature-aggregator usage and exit.
`

type Config struct {
	LogLevel           string             `mapstructure:"log-level" json:"log-level"`
	PChainAPI          *basecfg.APIConfig `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI            *basecfg.APIConfig `mapstructure:"info-api" json:"info-api"`
	APIPort            uint16             `mapstructure:"api-port" json:"api-port"`
	MetricsPort        uint16             `mapstructure:"metrics-port" json:"metrics-port"`
	SignatureCacheSize uint64             `mapstructure:"signature-cache-size" json:"signature-cache-size"`
	AllowPrivateIPs    bool               `mapstructure:"allow-private-ips" json:"allow-private-ips"`
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

var _ peers.Config = &Config{}

func (c *Config) GetPChainAPI() *basecfg.APIConfig {
	return c.PChainAPI
}

func (c *Config) GetInfoAPI() *basecfg.APIConfig {
	return c.InfoAPI
}

func (c *Config) GetAllowPrivateIPs() bool {
	return c.AllowPrivateIPs
}
