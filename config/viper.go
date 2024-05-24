// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/ava-labs/awm-relayer/utils"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Build the viper instance. The config file must be provided via the command line flag or environment variable.
// All config keys may be provided via config file or environment variable.
func BuildViper(fs *pflag.FlagSet) (*viper.Viper, error) {
	v := viper.New()
	v.AutomaticEnv()
	// Map flag names to env var names. Flags are capitalized, and hyphens are replaced with underscores.
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	if err := v.BindPFlags(fs); err != nil {
		return nil, err
	}

	// Verify required flags are set
	if !v.IsSet(ConfigFileKey) {
		DisplayUsageText()
		return nil, fmt.Errorf("config file not set")
	}

	filename := v.GetString(ConfigFileKey)
	v.SetConfigFile(filename)
	v.SetConfigType("json")
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	return v, nil
}

func SetDefaultConfigValues(v *viper.Viper) {
	v.SetDefault(LogLevelKey, defaultLogLevel)
	v.SetDefault(StorageLocationKey, defaultStorageLocation)
	v.SetDefault(ProcessMissedBlocksKey, defaultProcessMissedBlocks)
	v.SetDefault(APIPortKey, defaultAPIPort)
	v.SetDefault(MetricsPortKey, defaultMetricsPort)
	v.SetDefault(DBWriteIntervalSecondsKey, defaultIntervalSeconds)
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
	for i, subnet := range cfg.DestinationBlockchains {
		privateKey := subnet.AccountPrivateKey
		if accountPrivateKey != "" {
			optionOverwritten = true
			privateKey = accountPrivateKey
			// Otherwise, check for private keys suffixed with the chain ID and set it for that subnet
			// Since the key is dynamic, this is only possible through environment variables
		} else if privateKeyFromEnv := os.Getenv(fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, subnet.BlockchainID)); privateKeyFromEnv != "" {
			optionOverwritten = true
			privateKey = privateKeyFromEnv
		}
		cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(privateKey)
	}

	if err = cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("failed to validate configuration: %w", err)
	}

	return cfg, optionOverwritten, nil
}
