// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	baseCfg "github.com/ava-labs/awm-relayer/config"
)

const (
	cChainIdentifierString = "C"
	warpConfigKey          = "warpConfig"
)

const (
	defaultAPIPort = uint16(8080)
)

var defaultLogLevel = logging.Info.String()

const usageText = `
Usage:
signature-aggregator --config-file path-to-config                Specifies the config file and start the signing service.
signature-aggregator --version                                   Display signature-aggregator version and exit.
signature-aggregator --help                                      Display signature-aggregator usage and exit.
`

type Config struct {
	LogLevel          string              `mapstructure:"log-level" json:"log-level"`
	SourceBlockchains []*SourceBlockchain `mapstructure:"source-blockchains" json:"source-blockchains"`
	PChainAPI         *baseCfg.APIConfig  `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI           *baseCfg.APIConfig  `mapstructure:"info-api" json:"info-api"`
	APIPort           uint16              `mapstructure:"api-port" json:"api-port"`
}

func DisplayUsageText() {
	fmt.Printf("%s\n", usageText)
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters.
func (c *Config) Validate() error {
	if len(c.SourceBlockchains) == 0 {
		return errors.New("signature-aggregator not configured to sign messages from any subnets. A list of source subnets must be provided in the configuration file") //nolint:lll
	}
	if err := c.PChainAPI.Validate(); err != nil {
		return err
	}
	if err := c.InfoAPI.Validate(); err != nil {
		return err
	}

	// Validate the source chains and store the source subnet and chain IDs for future use
	sourceBlockchains := set.NewSet[string](len(c.SourceBlockchains))
	for _, s := range c.SourceBlockchains {
		if err := s.Validate(); err != nil {
			return err
		}
		// Verify uniqueness
		if sourceBlockchains.Contains(s.BlockchainID) {
			return errors.New("configured source subnets must have unique chain IDs")
		}
		sourceBlockchains.Add(s.BlockchainID)
	}

	return nil
}

func (c *Config) FromBaseConfig(cfg baseCfg.Config) (Config, error) {
	sourceBlockchains := make([]*SourceBlockchain, len(cfg.SourceBlockchains))
	for i, s := range cfg.SourceBlockchains {
		sourceBlockchain, err := FromBaseSourceBlockChain(s)
		if err != nil {
			return Config{}, err
		}
		sourceBlockchains[i] = sourceBlockchain
	}
	config := Config{
		LogLevel:          cfg.LogLevel,
		SourceBlockchains: sourceBlockchains,
		PChainAPI:         cfg.PChainAPI,
		InfoAPI:           cfg.InfoAPI,
		APIPort:           cfg.APIPort,
	}
	err := config.Validate()
	if err != nil {
		return Config{}, err
	}
	return config, nil
}
