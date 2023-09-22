// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertProtocol(t *testing.T) {
	testCases := []struct {
		name          string
		urlString     string
		protocol      string
		expectedUrl   string
		expectedError bool
	}{
		{
			name:          "valid http to https",
			urlString:     "http://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			name:          "valid https to http",
			urlString:     "https://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			name:          "valid http to http",
			urlString:     "http://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			name:          "valid https to https",
			urlString:     "https://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			name:          "invalid protocol",
			urlString:     "http://www.hello.com",
			protocol:      "\n",
			expectedUrl:   "",
			expectedError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualUrl, err := ConvertProtocol(testCase.urlString, testCase.protocol)

			if testCase.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expectedUrl, actualUrl)
			}
		})
	}
}

func TestSanitizeHashString(t *testing.T) {
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
		{
			name:           "return original length not divisible by 2",
			hash:           "0x1234g",
			expectedResult: "0x1234g",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualResult := SanitizeHashString(testCase.hash)
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
			actualResult := CheckStakeWeightExceedsThreshold(new(big.Int).SetUint64(testCase.accumulatedSignatureWeight), testCase.totalWeight, testCase.quorumNumerator, testCase.quorumDenominator)
			require.Equal(t, testCase.expectedResult, actualResult)
		})
	}
}
