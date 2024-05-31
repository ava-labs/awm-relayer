// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// AllAllowedAddress is used to construct relayer IDs when all addresses are allowed
	AllAllowedAddress = utils.ZeroAddress
)

// RelayerID is a unique identifier for an application relayer
type RelayerID struct {
	SourceBlockchainID      ids.ID
	DestinationBlockchainID ids.ID
	OriginSenderAddress     common.Address
	DestinationAddress      common.Address
	ID                      common.Hash
}

func NewRelayerID(
	sourceBlockchainID ids.ID,
	destinationBlockchainID ids.ID,
	originSenderAddress common.Address,
	destinationAddress common.Address,
) RelayerID {
	id := CalculateRelayerID(
		sourceBlockchainID,
		destinationBlockchainID,
		originSenderAddress,
		destinationAddress,
	)
	return RelayerID{
		SourceBlockchainID:      sourceBlockchainID,
		DestinationBlockchainID: destinationBlockchainID,
		OriginSenderAddress:     originSenderAddress,
		DestinationAddress:      destinationAddress,
		ID:                      id,
	}
}

// Standalone utility to calculate a relayer ID.
func CalculateRelayerID(
	sourceBlockchainID ids.ID,
	destinationBlockchainID ids.ID,
	originSenderAddress common.Address,
	destinationAddress common.Address,
) common.Hash {
	return crypto.Keccak256Hash(
		[]byte(strings.Join(
			[]string{
				sourceBlockchainID.String(),
				destinationBlockchainID.String(),
				originSenderAddress.String(),
				destinationAddress.String(),
			},
			"-",
		)),
	)
}

// Gets all of the possible relayer keys for a given configuration.
func GetConfigRelayerIDs(cfg *config.Config) []RelayerID {
	var keys []RelayerID
	for _, s := range cfg.SourceBlockchains {
		keys = append(keys, GetSourceBlockchainRelayerIDs(s)...)
	}
	return keys
}

// Calculates all of the possible relayer keys for a given source blockchain.
func GetSourceBlockchainRelayerIDs(sourceBlockchain *config.SourceBlockchain) []RelayerID {
	var ids []RelayerID
	srcAddresses := sourceBlockchain.GetAllowedOriginSenderAddresses()
	// If no addresses are provided, use the zero address to construct the relayer ID
	if len(srcAddresses) == 0 {
		srcAddresses = append(srcAddresses, AllAllowedAddress)
	}
	for _, srcAddress := range srcAddresses {
		for _, dst := range sourceBlockchain.SupportedDestinations {
			dstAddresses := dst.GetAddresses()
			// If no addresses are provided, use the zero address to construct the relayer ID
			if len(dstAddresses) == 0 {
				dstAddresses = append(dstAddresses, AllAllowedAddress)
			}
			for _, dstAddress := range dstAddresses {
				ids = append(ids, NewRelayerID(
					sourceBlockchain.GetBlockchainID(),
					dst.GetBlockchainID(),
					srcAddress,
					dstAddress,
				))
			}
		}
	}
	return ids
}
