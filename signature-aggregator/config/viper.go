// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func NewConfig(v *viper.Viper) (Config, error) {
	cfg, err := BuildConfig(v)
	if err != nil {
		return cfg, err
	}
	if err = cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("failed to validate configuration: %w", err)
	}
	return cfg, nil
}

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
	v.SetDefault(APIPortKey, defaultAPIPort)
	v.SetDefault(MetricsPortKey, defaultMetricsPort)
	v.SetDefault(
		SignatureCacheSizeKey,
		DefaultSignatureCacheSize,
	)
}

// BuildConfig constructs the signature aggregator config using Viper.
// The following precedence order is used. Each item takes precedence over the item below it:
//  1. Flags
//  2. Config file
//
// Returns the Config
func BuildConfig(v *viper.Viper) (Config, error) {
	// Set default values
	SetDefaultConfigValues(v)

	// Build the config from Viper
	var cfg Config

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal viper config: %w", err)
	}

	return cfg, nil
}
