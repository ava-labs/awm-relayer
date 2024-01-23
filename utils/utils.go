// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	// TODO: Revisit these constant values once we are using the subnet-evm branch with finalized
	// Warp implementation. Should evaluate the maximum gas used by the Teleporter contract "receiveCrossChainMessage"
	// method, excluding the call to execute the message payload.
	ReceiveCrossChainMessageStaticGasCost           uint64 = 2_000_000
	ReceiveCrossChainMessageGasCostPerAggregatedKey uint64 = 1_000
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

func ConvertProtocol(URLString, protocol string) (string, error) {
	var (
		u   *url.URL
		err error
	)
	if u, err = url.ParseRequestURI(URLString); err != nil {
		return "", fmt.Errorf("invalid url")
	}

	u.Scheme = protocol
	if _, err = url.ParseRequestURI(u.String()); err != nil {
		return "", fmt.Errorf("invalid protocol")
	}

	return u.String(), nil
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
