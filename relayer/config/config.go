// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	basecfg "github.com/ava-labs/icm-services/config"
	"github.com/ava-labs/icm-services/peers"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/subnet-evm/precompile/registry"
)

const (
	accountPrivateKeyEnvVarName = "ACCOUNT_PRIVATE_KEY"
	cChainIdentifierString      = "C"
	warpConfigKey               = "warpConfig"
	suppliedSubnetsLimit        = 16
)

const (
	defaultStorageLocation     = "./.icm-relayer-storage"
	defaultProcessMissedBlocks = true
	defaultAPIPort             = uint16(8080)
	defaultMetricsPort         = uint16(9090)
	defaultIntervalSeconds     = uint64(10)
	defaultSignatureCacheSize  = uint64(1024 * 1024)
)

var defaultLogLevel = logging.Info.String()

const usageText = `
Usage:
icm-relayer --config-file path-to-config                Specifies the relayer config file and begin relaying messages.
icm-relayer --version                                   Display icm-relayer version and exit.
icm-relayer --help                                      Display icm-relayer usage and exit.
`

// Top-level configuration
type Config struct {
	LogLevel               string                   `mapstructure:"log-level" json:"log-level"`
	StorageLocation        string                   `mapstructure:"storage-location" json:"storage-location"`
	RedisURL               string                   `mapstructure:"redis-url" json:"redis-url"`
	APIPort                uint16                   `mapstructure:"api-port" json:"api-port"`
	MetricsPort            uint16                   `mapstructure:"metrics-port" json:"metrics-port"`
	DBWriteIntervalSeconds uint64                   `mapstructure:"db-write-interval-seconds" json:"db-write-interval-seconds"` //nolint:lll
	PChainAPI              *basecfg.APIConfig       `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI                *basecfg.APIConfig       `mapstructure:"info-api" json:"info-api"`
	SourceBlockchains      []*SourceBlockchain      `mapstructure:"source-blockchains" json:"source-blockchains"`
	DestinationBlockchains []*DestinationBlockchain `mapstructure:"destination-blockchains" json:"destination-blockchains"`
	ProcessMissedBlocks    bool                     `mapstructure:"process-missed-blocks" json:"process-missed-blocks"`
	DeciderURL             string                   `mapstructure:"decider-url" json:"decider-url"`
	SignatureCacheSize     uint64                   `mapstructure:"signature-cache-size" json:"signature-cache-size"`
	ManuallyTrackedPeers   []*basecfg.PeerConfig    `mapstructure:"manually-tracked-peers" json:"manually-tracked-peers"`
	AllowPrivateIPs        bool                     `mapstructure:"allow-private-ips" json:"allow-private-ips"`

	// convenience field to fetch a blockchain's subnet ID
	blockchainIDToSubnetID map[ids.ID]ids.ID
	overwrittenOptions     []string
}

func DisplayUsageText() {
	fmt.Printf("%s\n", usageText)
}

func (c *Config) countSuppliedSubnets() int {
	foundSubnets := make(map[string]struct{})
	for _, sourceBlockchain := range c.SourceBlockchains {
		foundSubnets[sourceBlockchain.SubnetID] = struct{}{}
	}
	return len(foundSubnets)
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters.
func (c *Config) Validate() error {
	if len(c.SourceBlockchains) == 0 {
		return errors.New("relayer not configured to relay from any subnets. A list of source subnets must be provided in the configuration file") //nolint:lll
	}
	if suppliedSubnets := c.countSuppliedSubnets(); suppliedSubnets > suppliedSubnetsLimit {
		return fmt.Errorf("relayer can track at most %d subnets, %d are provided", suppliedSubnetsLimit, suppliedSubnets)
	}
	if len(c.DestinationBlockchains) == 0 {
		return errors.New("relayer not configured to relay to any subnets. A list of destination subnets must be provided in the configuration file") //nolint:lll
	}
	if err := c.PChainAPI.Validate(); err != nil {
		return err
	}
	if err := c.InfoAPI.Validate(); err != nil {
		return err
	}
	if c.DBWriteIntervalSeconds == 0 || c.DBWriteIntervalSeconds > 600 {
		return errors.New("db-write-interval-seconds must be between 1 and 600")
	}
	for _, p := range c.ManuallyTrackedPeers {
		if err := p.Validate(); err != nil {
			return err
		}
	}

	blockchainIDToSubnetID := make(map[ids.ID]ids.ID)

	// Validate the destination chains
	destinationChains := set.NewSet[string](len(c.DestinationBlockchains))
	for _, s := range c.DestinationBlockchains {
		if err := s.Validate(); err != nil {
			return err
		}
		if destinationChains.Contains(s.BlockchainID) {
			return errors.New("configured destination subnets must have unique chain IDs")
		}
		destinationChains.Add(s.BlockchainID)
		blockchainIDToSubnetID[s.blockchainID] = s.subnetID
	}

	// Validate the source chains and store the source subnet and chain IDs for future use
	sourceBlockchains := set.NewSet[string](len(c.SourceBlockchains))
	for _, s := range c.SourceBlockchains {
		// Validate configuration
		if err := s.Validate(&destinationChains); err != nil {
			return err
		}
		// Verify uniqueness
		if sourceBlockchains.Contains(s.BlockchainID) {
			return errors.New("configured source subnets must have unique chain IDs")
		}
		sourceBlockchains.Add(s.BlockchainID)
		blockchainIDToSubnetID[s.blockchainID] = s.subnetID
	}
	c.blockchainIDToSubnetID = blockchainIDToSubnetID

	if len(c.DeciderURL) != 0 {
		if _, err := url.ParseRequestURI(c.DeciderURL); err != nil {
			return fmt.Errorf("Invalid decider URL: %w", err)
		}
	}

	return nil
}

func (c *Config) GetSubnetID(blockchainID ids.ID) ids.ID {
	return c.blockchainIDToSubnetID[blockchainID]
}

// If the numerator in the Warp config is 0, use the default value
func warpConfigFromSubnetWarpConfig(inputConfig warp.Config) WarpConfig {
	if inputConfig.QuorumNumerator == 0 {
		return WarpConfig{
			QuorumNumerator:              warp.WarpDefaultQuorumNumerator,
			RequirePrimaryNetworkSigners: inputConfig.RequirePrimaryNetworkSigners,
		}
	}
	return WarpConfig{
		QuorumNumerator:              inputConfig.QuorumNumerator,
		RequirePrimaryNetworkSigners: inputConfig.RequirePrimaryNetworkSigners,
	}
}

func getWarpConfig(client ethclient.Client) (*warp.Config, error) {
	// Fetch the subnet's chain config
	chainConfig, err := client.ChainConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chain config")
	}

	// First, check the list of precompile upgrades to get the most up to date Warp config
	// We only need to consider the most recent Warp config, since the QuorumNumerator is used
	// at signature verification time on the receiving chain, regardless of the Warp config at the
	// time of the message's creation
	var warpConfig *warp.Config
	for _, precompile := range chainConfig.UpgradeConfig.PrecompileUpgrades {
		cfg, ok := precompile.Config.(*warp.Config)
		if !ok {
			continue
		}
		if warpConfig == nil {
			warpConfig = cfg
			continue
		}
		if *cfg.Timestamp() > *warpConfig.Timestamp() {
			warpConfig = cfg
		}
	}
	if warpConfig != nil {
		return warpConfig, nil
	}
	// If we didn't find the Warp config in the upgrade precompile list, check the genesis config
	warpConfig, ok := chainConfig.GenesisPrecompiles[warpConfigKey].(*warp.Config)
	if !ok {
		return nil, fmt.Errorf("no Warp config found in chain config")
	}
	return warpConfig, nil
}

// Initializes Warp configurations (quorum and self-signing settings) for each destination subnet
func (c *Config) InitializeWarpConfigs() error {
	// Fetch the Warp config values for each destination subnet.
	for _, destinationSubnet := range c.DestinationBlockchains {
		err := destinationSubnet.initializeWarpConfigs()
		if err != nil {
			return fmt.Errorf(
				"failed to initialize Warp config for destination subnet %s: %w",
				destinationSubnet.SubnetID,
				err,
			)
		}
	}

	return nil
}

func (c *Config) HasOverwrittenOptions() bool {
	return len(c.overwrittenOptions) > 0
}

func (c *Config) GetOverwrittenOptions() []string {
	return c.overwrittenOptions
}

//
// Top-level config getters
//

func (c *Config) GetWarpConfig(blockchainID ids.ID) (WarpConfig, error) {
	for _, s := range c.DestinationBlockchains {
		if blockchainID.String() == s.BlockchainID {
			return s.warpConfig, nil
		}
	}
	return WarpConfig{}, fmt.Errorf("blockchain %s not configured as a destination", blockchainID)
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
