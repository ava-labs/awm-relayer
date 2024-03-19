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

const (
	LatestProcessedBlockKey = "latestProcessedBlock"
)

var (
	ErrKeyNotFound              = errors.New("key not found")
	ErrChainNotFound            = errors.New("no database for chain")
	ErrDatabaseMisconfiguration = errors.New("database misconfiguration")
)

// RelayerDatabase is a key-value store for relayer state, with each blockchainID maintaining its own state
type RelayerDatabase interface {
	Get(relayerKey common.Hash, key []byte) ([]byte, error)
	Put(relayerKey common.Hash, key []byte, value []byte) error
}

type RelayerKey struct {
	SourceBlockchainID      ids.ID
	DestinationBlockchainID ids.ID
	OriginSenderAddress     common.Address
	DestinationAddress      common.Address
}

func (k RelayerKey) CalculateRelayerKey() common.Hash {
	return CalculateRelayerKey(
		k.SourceBlockchainID,
		k.DestinationBlockchainID,
		k.OriginSenderAddress,
		k.DestinationAddress,
	)
}

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

func GetConfigRelayerKeys(cfg *config.Config) []RelayerKey {
	var keys []RelayerKey
	for _, s := range cfg.SourceBlockchains {
		keys = append(keys, GetSourceConfigRelayerKeys(s)...)
	}
	return keys
}
func GetSourceConfigRelayerKeys(cfg *config.SourceBlockchain) []RelayerKey {
	var keys []RelayerKey
	for _, dst := range cfg.GetSupportedDestinations().List() {
		keys = append(keys, RelayerKey{
			SourceBlockchainID:      cfg.GetBlockchainID(),
			DestinationBlockchainID: dst,
			OriginSenderAddress:     common.Address{}, // TODO: populate with allowed sender/receiver addresses
			DestinationAddress:      common.Address{},
		})
	}
	return keys
}
