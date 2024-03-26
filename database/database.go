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
	ErrDataKeyNotFound          = errors.New("data key not found")
	ErrRelayerKeyNotFound       = errors.New("no database for relayer key")
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

// RelayerDatabase is a key-value store for relayer state, with each relayerKey maintaining its own state
type RelayerDatabase interface {
	Get(relayerKey common.Hash, dataKey DataKey) ([]byte, error)
	Put(relayerKey common.Hash, dataKey DataKey, value []byte) error
}

// Returns true if an error returned by a RelayerDatabase indicates the requested key was not found
func IsKeyNotFoundError(err error) bool {
	return errors.Is(err, ErrRelayerKeyNotFound) || errors.Is(err, ErrDataKeyNotFound)
}

// RelayerKey is a unique identifier for an application relayer
type RelayerKey struct {
	SourceBlockchainID      ids.ID
	DestinationBlockchainID ids.ID
	OriginSenderAddress     common.Address
	DestinationAddress      common.Address
	key                     common.Hash
}

// GetKey returns the unique identifier for an application relayer
func (k *RelayerKey) GetKey() common.Hash {
	if k.key == (common.Hash{}) {
		k.key = CalculateRelayerKey(
			k.SourceBlockchainID,
			k.DestinationBlockchainID,
			k.OriginSenderAddress,
			k.DestinationAddress,
		)
	}
	return k.key
}

// Standalone utility to calculate a relayer key
func CalculateRelayerKey(
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
func GetConfigRelayerKeys(cfg *config.Config) []RelayerKey {
	var keys []RelayerKey
	for _, s := range cfg.SourceBlockchains {
		keys = append(keys, GetSourceBlockchainRelayerKeys(s)...)
	}
	return keys
}

// Calculate all of the possible relayer keys for a given source blockchain
func GetSourceBlockchainRelayerKeys(sourceBlockchain *config.SourceBlockchain) []RelayerKey {
	var keys []RelayerKey
	for _, dst := range sourceBlockchain.GetSupportedDestinations().List() {
		keys = append(keys, RelayerKey{
			SourceBlockchainID:      sourceBlockchain.GetBlockchainID(),
			DestinationBlockchainID: dst,
			OriginSenderAddress:     common.Address{}, // TODO: populate with allowed sender/receiver addresses
			DestinationAddress:      common.Address{},
		})
	}
	return keys
}
