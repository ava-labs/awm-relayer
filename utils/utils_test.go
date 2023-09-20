// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertProtocol(t *testing.T) {
	testCases := []struct {
		urlString     string
		protocol      string
		expectedUrl   string
		expectedError bool
	}{
		{
			urlString:     "http://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "https://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "http://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "https://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "http://www.hello.com",
			protocol:      "\n",
			expectedUrl:   "",
			expectedError: true,
		},
	}

	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
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
		hash           string
		expectedResult string
	}{
		// Remove leading 0x from hex string
		{
			hash:           "0x1234",
			expectedResult: "1234",
		},
		// Return original hex string
		{
			hash:           "1234",
			expectedResult: "1234",
		},
		// Return original string, leading 0x is not hex
		{
			hash:           "0x1234g",
			expectedResult: "0x1234g",
		},
	}
	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			actualResult := SanitizeHashString(testCase.hash)
			require.Equal(t, testCase.expectedResult, actualResult)
		})
	}
}

func TestCheckStakeWeightExceedsThreshold(t *testing.T) {
	testCases := []struct {
		accumulatedSignatureWeight uint64
		totalWeight                uint64
		quorumNumerator            uint64
		quorumDenominator          uint64
		expectedResult             bool
	}{
		{
			accumulatedSignatureWeight: 0,
			totalWeight:                0,
			quorumNumerator:            0,
			quorumDenominator:          0,
			expectedResult:             true,
		},
		{
			accumulatedSignatureWeight: 67_000_000,
			totalWeight:                100_000_000,
			quorumNumerator:            67,
			quorumDenominator:          100,
			expectedResult:             true,
		},
		{
			accumulatedSignatureWeight: 66_999_999,
			totalWeight:                100_000_000,
			quorumNumerator:            67,
			quorumDenominator:          100,
			expectedResult:             false,
		},
		{
			accumulatedSignatureWeight: 67_000_000,
			totalWeight:                67_000_000,
			quorumNumerator:            100,
			quorumDenominator:          100,
			expectedResult:             true,
		},
		{
			accumulatedSignatureWeight: 66_999_999,
			totalWeight:                67_000_000,
			quorumNumerator:            100,
			quorumDenominator:          100,
			expectedResult:             false,
		},
	}
	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			actualResult := CheckStakeWeightExceedsThreshold(new(big.Int).SetUint64(testCase.accumulatedSignatureWeight), testCase.totalWeight, testCase.quorumNumerator, testCase.quorumDenominator)
			require.Equal(t, testCase.expectedResult, actualResult)
		})
	}
}
