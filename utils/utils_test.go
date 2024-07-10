// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHexOrCB58ToID(t *testing.T) {
	testCases := []struct {
		name           string
		encoding       string
		expectedResult string
		errorExpected  bool
	}{
		{
			name:           "hex conversion",
			encoding:       "0x7fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d5",
			expectedResult: "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
			errorExpected:  false,
		},
		{
			name:           "cb58 conversion",
			encoding:       "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
			expectedResult: "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
			errorExpected:  false,
		},
		{
			name:          "non-prefixed hex",
			encoding:      "7fc93d85c6d62c5b2ac0b519c87010ea5294012d1e407030d6acd0021cac10d5",
			errorExpected: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualResult, err := HexOrCB58ToID(testCase.encoding)
			if testCase.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedResult, actualResult.String())
			}
		})
	}
}

func TestSanitizeHexString(t *testing.T) {
	testCases := []struct {
		name           string
		hash           string
		expectedResult string
	}{
		{
			name:           "remove leading 0x",
			hash:           "0x1234",
			expectedResult: "1234",
		},
		{
			name:           "return original non leading 0x",
			hash:           "1234",
			expectedResult: "1234",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualResult := SanitizeHexString(testCase.hash)
			require.Equal(t, testCase.expectedResult, actualResult)
		})
	}
}

func TestCheckStakeWeightExceedsThreshold(t *testing.T) {
	testCases := []struct {
		name                       string
		accumulatedSignatureWeight uint64
		totalWeight                uint64
		quorumNumerator            uint64
		quorumDenominator          uint64
		expectedResult             bool
	}{
		{
			name:                       "zero case",
			accumulatedSignatureWeight: 0,
			totalWeight:                0,
			quorumNumerator:            0,
			quorumDenominator:          0,
			expectedResult:             true,
		},
		{
			name:                       "valid case",
			accumulatedSignatureWeight: 67_000_000,
			totalWeight:                100_000_000,
			quorumNumerator:            67,
			quorumDenominator:          100,
			expectedResult:             true,
		},
		{
			name:                       "invalid case",
			accumulatedSignatureWeight: 66_999_999,
			totalWeight:                100_000_000,
			quorumNumerator:            67,
			quorumDenominator:          100,
			expectedResult:             false,
		},
		{
			name:                       "valid 100 percent case",
			accumulatedSignatureWeight: 67_000_000,
			totalWeight:                67_000_000,
			quorumNumerator:            100,
			quorumDenominator:          100,
			expectedResult:             true,
		},
		{
			name:                       "invalid 100 percent case",
			accumulatedSignatureWeight: 66_999_999,
			totalWeight:                67_000_000,
			quorumNumerator:            100,
			quorumDenominator:          100,
			expectedResult:             false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualResult := CheckStakeWeightExceedsThreshold(
				new(big.Int).SetUint64(testCase.accumulatedSignatureWeight),
				testCase.totalWeight,
				testCase.quorumNumerator,
				testCase.quorumDenominator,
			)
			require.Equal(t, testCase.expectedResult, actualResult)
		})
	}
}
