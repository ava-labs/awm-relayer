// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var id1 ids.ID = ids.GenerateTestID()
var id2 ids.ID = ids.GenerateTestID()

func TestCheckSupportedDestination(t *testing.T) {
	testCases := []struct {
		name               string
		relayer            Relayer
		destinationChainID ids.ID
		expectedResult     bool
	}{
		{
			name: "explicitly supported destination",
			relayer: Relayer{
				supportedDestinations: set.Set[ids.ID]{
					id1: {},
				},
			},
			destinationChainID: id1,
			expectedResult:     true,
		},
		{
			name:               "implicitly supported destination",
			relayer:            Relayer{},
			destinationChainID: id1,
			expectedResult:     true,
		},
		{
			name: "unsupported destination",
			relayer: Relayer{
				supportedDestinations: set.Set[ids.ID]{
					id1: {},
				},
			},
			destinationChainID: id2,
			expectedResult:     false,
		},
	}

	for _, testCase := range testCases {
		result := testCase.relayer.CheckSupportedDestination(testCase.destinationChainID)
		if result != testCase.expectedResult {
			t.Fatalf("Test '%s' failed: expected result %v, got %v", testCase.name, testCase.expectedResult, result)
		}
	}
}
