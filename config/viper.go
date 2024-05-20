// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"strings"

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
