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
	id                      common.Hash
}

// GetKey returns the unique identifier for an application relayer
func (id *RelayerID) GetID() common.Hash {
	if id.id == (common.Hash{}) {
		id.id = CalculateRelayerID(
			id.SourceBlockchainID,
			id.DestinationBlockchainID,
			id.OriginSenderAddress,
			id.DestinationAddress,
		)
	}
	return id.id
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
		keys = append(keys, GetSourceBlockchainRelayerIDs(s, cfg)...)
	}
	return keys
}

// Calculate all of the possible relayer keys for a given source blockchain
func GetSourceBlockchainRelayerIDs(sourceBlockchain *config.SourceBlockchain, cfg *config.Config) []RelayerID {
	var ids []RelayerID
	for _, srcAddress := range cfg.GetSourceBlockchainAllowedAddresses(sourceBlockchain.GetBlockchainID()) {
		for _, dstID := range sourceBlockchain.GetSupportedDestinations().List() {
			for _, dstAddress := range cfg.GetDestinationBlockchainAllowedAddresses(dstID) {
				ids = append(ids, RelayerID{
					SourceBlockchainID:      sourceBlockchain.GetBlockchainID(),
					DestinationBlockchainID: dstID,
					OriginSenderAddress:     srcAddress,
					DestinationAddress:      dstAddress,
				})
			}
		}
	}
	return ids
}
