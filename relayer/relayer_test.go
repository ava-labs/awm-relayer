// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"testing"

	"github.com/ava-labs/awm-relayer/config"
	"github.com/stretchr/testify/require"
)

func TestGetRelayerAccountInfoSkipChainConfigCheckCompatible(t *testing.T) {
	accountPrivateKey := "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
	expectedAddress := "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"

	info := config.DestinationSubnet{
		AccountPrivateKey: accountPrivateKey,
	}
	_, address, err := info.GetRelayerAccountInfo()

	require.NoError(t, err)
	require.Equal(t, expectedAddress, address.String())
}
