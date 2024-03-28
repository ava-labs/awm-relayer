// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

var (
	// Errors
	ErrNilInput = errors.New("nil input")
	ErrTooLarge = errors.New("exceeds uint256 maximum value")
)

//
// AWM Utils
//

// CheckStakeWeightExceedsThreshold returns true if the accumulated signature weight is at least [quorumNum]/[quorumDen] of [totalWeight].
func CheckStakeWeightExceedsThreshold(accumulatedSignatureWeight *big.Int, totalWeight uint64, quorumNumerator uint64, quorumDenominator uint64) bool {
	if accumulatedSignatureWeight == nil {
		return false
	}

	// Verifies that quorumNum * totalWeight <= quorumDen * sigWeight
	totalWeightBI := new(big.Int).SetUint64(totalWeight)
	scaledTotalWeight := new(big.Int).Mul(totalWeightBI, new(big.Int).SetUint64(quorumNumerator))
	scaledSigWeight := new(big.Int).Mul(accumulatedSignatureWeight, new(big.Int).SetUint64(quorumDenominator))

	return scaledTotalWeight.Cmp(scaledSigWeight) != 1
}

//
// Generic Utils
//

// BigToHashSafe ensures that a bignum value is able to fit into a 32 byte buffer before converting it to a common.Hash
// Returns an error if overflow/truncation would occur by trying to perfom this operation.
func BigToHashSafe(in *big.Int) (common.Hash, error) {
	if in == nil {
		return common.Hash{}, ErrNilInput
	}

	bytes := in.Bytes()
	if len(bytes) > common.HashLength {
		return common.Hash{}, ErrTooLarge
	}

	return common.BytesToHash(bytes), nil
}

// SanitizeHexString removes the "0x" prefix from a hex string if it exists.
// Otherwise, returns the original string.
func SanitizeHexString(hex string) string {
	return strings.TrimPrefix(hex, "0x")
}

// StripFromString strips the input string starting from the first occurrence of the substring.
func StripFromString(input, substring string) string {
	index := strings.Index(input, substring)
	if index == -1 {
		// Substring not found, return the original string
		return input
	}

	// Strip the string starting from the found substring
	strippedString := input[:index]

	return strippedString
}
