// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestNewTxSigner(t *testing.T) {
	type retStruct struct {
		pk   *ecdsa.PrivateKey
		addr common.Address
		err  error
	}

	testCases := []struct {
		name           string
		dst            config.DestinationBlockchain
		expectedResult retStruct
	}{
		{
			name: "valid",
			dst: config.DestinationBlockchain{
				AccountPrivateKey: "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
			expectedResult: retStruct{
				pk: &ecdsa.PrivateKey{
					D: big.NewInt(-5567472993773453273),
				},
				addr: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				err:  nil,
			},
		},
		{
			name: "invalid private key",
			dst: config.DestinationBlockchain{
				AccountPrivateKey: "invalid56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
			expectedResult: retStruct{
				pk: &ecdsa.PrivateKey{
					D: big.NewInt(-5567472993773453273),
				},
				addr: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				err:  utils.ErrInvalidPrivateKeyHex,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			txSigner, err := NewTxSigner(&testCase.dst)
			require.Equal(t, testCase.expectedResult.err, err)
			if err == nil {
				require.Equal(t, testCase.expectedResult.pk.D.Int64(), txSigner.pk.D.Int64())
				require.Equal(t, testCase.expectedResult.addr, txSigner.eoa)
			}
		})
	}
}
