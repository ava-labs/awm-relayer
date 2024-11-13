// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "github.com/spf13/pflag"

func BuildFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("icm-relayer", pflag.ContinueOnError)
	fs.String(ConfigFileKey, "", "Specifies the icm-relayer config file")
	fs.BoolP(VersionKey, "", false, "Display icm-relayer version")
	fs.BoolP(HelpKey, "", false, "Display icm-relayer usage")
	return fs
}
