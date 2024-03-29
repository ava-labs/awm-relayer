// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package database

import (
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
)

var (
	ErrKeyNotFound              = errors.New("key not found")
	ErrRelayerIDNotFound        = errors.New("no database entryfor relayer id")
	ErrDatabaseMisconfiguration = errors.New("database misconfiguration")

	// AllAllowedAddress is used to construct relayer IDs when all addresses are allowed
	AllAllowedAddress = common.Address{}
)

const (
	LatestProcessedBlockKey DataKey = iota
)

type DataKey int

func (k DataKey) String() string {
	switch k {
	case LatestProcessedBlockKey:
		return "latestProcessedBlock"
	}
	return "unknown"
}

// RelayerDatabase is a key-value store for relayer state, with each relayerID maintaining its own state
type RelayerDatabase interface {
	Get(relayerID common.Hash, key DataKey) ([]byte, error)
	Put(relayerID common.Hash, key DataKey, value []byte) error
}

// Returns true if an error returned by a RelayerDatabase indicates the requested key was not found
func IsKeyNotFoundError(err error) bool {
	return errors.Is(err, ErrRelayerIDNotFound) || errors.Is(err, ErrKeyNotFound)
}

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

// Standalone utility to calculate a relayer ID
func CalculateRelayerID(
	sourceBlockchainID ids.ID,
	destinationBlockchainID ids.ID,
	originSenderAddress common.Address,
	desinationAddress common.Address,
) common.Hash {
	return crypto.Keccak256Hash(
		[]byte(strings.Join(
			[]string{
				sourceBlockchainID.String(),
				destinationBlockchainID.String(),
				originSenderAddress.String(),
				desinationAddress.String(),
			},
			"-",
		)),
	)
}

// Get all of the possible relayer keys for a given configuration
func GetConfigRelayerIDs(cfg *config.Config) []RelayerID {
	var keys []RelayerID
	for _, s := range cfg.SourceBlockchains {
		keys = append(keys, GetSourceBlockchainRelayerIDs(s)...)
	}
	return keys
}

// Calculate all of the possible relayer keys for a given source blockchain
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
