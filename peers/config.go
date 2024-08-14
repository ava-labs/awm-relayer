// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import "github.com/ava-labs/awm-relayer/config"

// Config defines a common interface necessary for standing up an AppRequestNetwork.
type Config interface {
	GetInfoAPI() *config.APIConfig
	GetPChainAPI() *config.APIConfig
}
