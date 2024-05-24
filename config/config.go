// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/utils"

	"github.com/ava-labs/awm-relayer/ethclient"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/subnet-evm/precompile/registry"

	"github.com/spf13/viper"
)

const (
	accountPrivateKeyEnvVarName = "ACCOUNT_PRIVATE_KEY"
	cChainIdentifierString      = "C"
	warpConfigKey               = "warpConfig"
)

const usageText = `
Usage:
awm-relayer --config-file path-to-config                Specifies the relayer config file and begin relaying messages.
awm-relayer --version                                   Display awm-relayer version and exit.
awm-relayer --help                                      Display awm-relayer usage and exit.
`

var errFailedToGetWarpQuorum = errors.New("failed to get warp quorum")

// API configuration containing the base URL and query parameters
type APIConfig struct {
	BaseURL     string            `mapstructure:"base-url" json:"base-url"`
	QueryParams map[string]string `mapstructure:"query-parameters" json:"query-parameters"`
	HTTPHeaders map[string]string `mapstructure:"http-headers" json:"http-headers"`
}

// Top-level configuration
type Config struct {
	LogLevel               string                   `mapstructure:"log-level" json:"log-level"`
	StorageLocation        string                   `mapstructure:"storage-location" json:"storage-location"`
	RedisURL               string                   `mapstructure:"redis-url" json:"redis-url"`
	APIPort                uint16                   `mapstructure:"api-port" json:"api-port"`
	MetricsPort            uint16                   `mapstructure:"metrics-port" json:"metrics-port"`
	DBWriteIntervalSeconds uint64                   `mapstructure:"db-write-interval-seconds" json:"db-write-interval-seconds"`
	PChainAPI              *APIConfig               `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI                *APIConfig               `mapstructure:"info-api" json:"info-api"`
	SourceBlockchains      []*SourceBlockchain      `mapstructure:"source-blockchains" json:"source-blockchains"`
	DestinationBlockchains []*DestinationBlockchain `mapstructure:"destination-blockchains" json:"destination-blockchains"`
	ProcessMissedBlocks    bool                     `mapstructure:"process-missed-blocks" json:"process-missed-blocks"`
	ManualWarpMessages     []*ManualWarpMessage     `mapstructure:"manual-warp-messages" json:"manual-warp-messages"`

	// convenience field to fetch a blockchain's subnet ID
	blockchainIDToSubnetID map[ids.ID]ids.ID
}

func SetDefaultConfigValues(v *viper.Viper) {
	v.SetDefault(LogLevelKey, logging.Info.String())
	v.SetDefault(StorageLocationKey, "./.awm-relayer-storage")
	v.SetDefault(ProcessMissedBlocksKey, true)
	v.SetDefault(APIPortKey, 8080)
	v.SetDefault(MetricsPortKey, 9090)
	v.SetDefault(DBWriteIntervalSecondsKey, 10)
}

func DisplayUsageText() {
	fmt.Printf("%s\n", usageText)
}

// BuildConfig constructs the relayer config using Viper.
// The following precedence order is used. Each item takes precedence over the item below it:
//  1. Flags
//  2. Environment variables
//     a. Global account-private-key
//     b. Chain-specific account-private-key
//  3. Config file
//
// Returns the Config option and a bool indicating whether any options provided from one source
// were explicitly overridden by a higher precedence source.
// TODO: Improve the optionOverwritten return value to reflect the key that was modified.
func BuildConfig(v *viper.Viper) (Config, bool, error) {
	// Set default values
	SetDefaultConfigValues(v)

	// Build the config from Viper
	var (
		cfg               Config
		err               error
		optionOverwritten bool = false
	)

	cfg.LogLevel = v.GetString(LogLevelKey)
	cfg.StorageLocation = v.GetString(StorageLocationKey)
	cfg.RedisURL = v.GetString(RedisURLKey)
	cfg.ProcessMissedBlocks = v.GetBool(ProcessMissedBlocksKey)
	cfg.APIPort = v.GetUint16(APIPortKey)
	cfg.MetricsPort = v.GetUint16(MetricsPortKey)
	cfg.DBWriteIntervalSeconds = v.GetUint64(DBWriteIntervalSecondsKey)
	if err := v.UnmarshalKey(PChainAPIKey, &cfg.PChainAPI); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal P-Chain API: %w", err)
	}
	if err := v.UnmarshalKey(InfoAPIKey, &cfg.InfoAPI); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal Info API: %w", err)
	}
	if err := v.UnmarshalKey(ManualWarpMessagesKey, &cfg.ManualWarpMessages); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal manual warp messages: %w", err)
	}
	if err := v.UnmarshalKey(DestinationBlockchainsKey, &cfg.DestinationBlockchains); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal destination subnets: %w", err)
	}
	if err := v.UnmarshalKey(SourceBlockchainsKey, &cfg.SourceBlockchains); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal source subnets: %w", err)
	}

	// Explicitly overwrite the configured account private key
	// If account-private-key is set as a flag or environment variable,
	// overwrite all destination subnet configurations to use that key
	// In all cases, sanitize the key before setting it in the config
	accountPrivateKey := v.GetString(AccountPrivateKeyKey)
	if accountPrivateKey != "" {
		optionOverwritten = true
		for i := range cfg.DestinationBlockchains {
			cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(accountPrivateKey)
		}
	} else {
		// Otherwise, check for private keys suffixed with the chain ID and set it for that subnet
		// Since the key is dynamic, this is only possible through environment variables
		for i, subnet := range cfg.DestinationBlockchains {
			subnetAccountPrivateKey := os.Getenv(fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, subnet.BlockchainID))
			if subnetAccountPrivateKey != "" {
				optionOverwritten = true
				cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(subnetAccountPrivateKey)
			} else {
				cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(cfg.DestinationBlockchains[i].AccountPrivateKey)
			}
		}
	}

	if err = cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("failed to validate configuration: %w", err)
	}

	return cfg, optionOverwritten, nil
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters
func (c *Config) Validate() error {
	if len(c.SourceBlockchains) == 0 {
		return errors.New("relayer not configured to relay from any subnets. A list of source subnets must be provided in the configuration file")
	}
	if len(c.DestinationBlockchains) == 0 {
		return errors.New("relayer not configured to relay to any subnets. A list of destination subnets must be provided in the configuration file")
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

	// Validate the manual warp messages
	for i, msg := range c.ManualWarpMessages {
		if err := msg.Validate(); err != nil {
			return fmt.Errorf("invalid manual warp message at index %d: %w", i, err)
		}
	}

	return nil
}

func (c *Config) GetSubnetID(blockchainID ids.ID) ids.ID {
	return c.blockchainIDToSubnetID[blockchainID]
}

// If the numerator in the Warp config is 0, use the default value
func calculateQuorumNumerator(cfgNumerator uint64) uint64 {
	if cfgNumerator == 0 {
		return warp.WarpDefaultQuorumNumerator
	}
	return cfgNumerator
}

// Helper to retrieve the Warp Quorum from the chain config.
// Differentiates between subnet-evm and coreth RPC internally
func getWarpQuorum(
	subnetID ids.ID,
	blockchainID ids.ID,
	client ethclient.Client,
) (WarpQuorum, error) {
	if subnetID == constants.PrimaryNetworkID {
		return WarpQuorum{
			QuorumNumerator:   warp.WarpDefaultQuorumNumerator,
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}

	// Fetch the subnet's chain config
	chainConfig, err := client.ChainConfig(context.Background())
	if err != nil {
		return WarpQuorum{}, fmt.Errorf("failed to fetch chain config for blockchain %s: %w", blockchainID, err)
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
		return WarpQuorum{
			QuorumNumerator:   calculateQuorumNumerator(warpConfig.QuorumNumerator),
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}

	// If we didn't find the Warp config in the upgrade precompile list, check the genesis config
	warpConfig, ok := chainConfig.GenesisPrecompiles[warpConfigKey].(*warp.Config)
	if ok {
		return WarpQuorum{
			QuorumNumerator:   calculateQuorumNumerator(warpConfig.QuorumNumerator),
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}
	return WarpQuorum{}, fmt.Errorf("failed to find warp config for blockchain %s", blockchainID)
}

func (c *Config) InitializeWarpQuorums() error {
	// Fetch the Warp quorum values for each destination subnet.
	for _, destinationSubnet := range c.DestinationBlockchains {
		err := destinationSubnet.initializeWarpQuorum()
		if err != nil {
			return fmt.Errorf("failed to initialize Warp quorum for destination subnet %s: %w", destinationSubnet.SubnetID, err)
		}
	}

	return nil
}

func (c *APIConfig) Validate() error {
	if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	return nil
}

//
// Top-level config getters
//

func (c *Config) GetWarpQuorum(blockchainID ids.ID) (WarpQuorum, error) {
	for _, s := range c.DestinationBlockchains {
		if blockchainID.String() == s.BlockchainID {
			return s.warpQuorum, nil
		}
	}
	return WarpQuorum{}, errFailedToGetWarpQuorum
}
