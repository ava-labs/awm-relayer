// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
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
)

const (
	defaultStorageLocation     = "./.awm-relayer-storage"
	defaultProcessMissedBlocks = true
	defaultAPIPort             = uint16(8080)
	defaultMetricsPort         = uint16(9090)
	defaultIntervalSeconds     = uint64(10)
)

var defaultLogLevel = logging.Info.String()

const usageText = `
Usage:
awm-relayer --config-file path-to-config                Specifies the relayer config file and begin relaying messages.
awm-relayer --version                                   Display awm-relayer version and exit.
awm-relayer --help                                      Display awm-relayer usage and exit.
`

var errFailedToGetWarpQuorum = errors.New("failed to get warp quorum")

// Top-level configuration
type Config struct {
	LogLevel               string                   `mapstructure:"log-level" json:"log-level"`
	StorageLocation        string                   `mapstructure:"storage-location" json:"storage-location"`
	RedisURL               string                   `mapstructure:"redis-url" json:"redis-url"`
	APIPort                uint16                   `mapstructure:"api-port" json:"api-port"`
	MetricsPort            uint16                   `mapstructure:"metrics-port" json:"metrics-port"`
	DBWriteIntervalSeconds uint64                   `mapstructure:"db-write-interval-seconds" json:"db-write-interval-seconds"` //nolint:lll
	PChainAPI              *APIConfig               `mapstructure:"p-chain-api" json:"p-chain-api"`
	InfoAPI                *APIConfig               `mapstructure:"info-api" json:"info-api"`
	SourceBlockchains      []*SourceBlockchain      `mapstructure:"source-blockchains" json:"source-blockchains"`
	DestinationBlockchains []*DestinationBlockchain `mapstructure:"destination-blockchains" json:"destination-blockchains"`
	ProcessMissedBlocks    bool                     `mapstructure:"process-missed-blocks" json:"process-missed-blocks"`
	DeciderHost            string                   `mapstructure:"decider-host" json:"decider-host"`
	DeciderPort            *uint16                  `mapstructure:"decider-port" json:"decider-port"`

	// convenience field to fetch a blockchain's subnet ID
	blockchainIDToSubnetID map[ids.ID]ids.ID
	overwrittenOptions     []string
}

func DisplayUsageText() {
	fmt.Printf("%s\n", usageText)
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters.
func (c *Config) Validate() error {
	if len(c.SourceBlockchains) == 0 {
		return errors.New("relayer not configured to relay from any subnets. A list of source subnets must be provided in the configuration file") //nolint:lll
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

	if c.DeciderPort != nil {
		portStr := strconv.FormatUint(uint64(*c.DeciderPort), 10)

		host := c.DeciderHost
		if len(host) == 0 {
			host = "localhost"
		}

		uri := strings.Join([]string{host, portStr}, ":")

		_, err := url.ParseRequestURI(uri)
		if err != nil {
			return fmt.Errorf("Invalid decider URI: %w", err)
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
			return fmt.Errorf(
				"failed to initialize Warp quorum for destination subnet %s: %w",
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

func (c *Config) GetWarpQuorum(blockchainID ids.ID) (WarpQuorum, error) {
	for _, s := range c.DestinationBlockchains {
		if blockchainID.String() == s.BlockchainID {
			return s.warpQuorum, nil
		}
	}
	return WarpQuorum{}, errFailedToGetWarpQuorum
}
