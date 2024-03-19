// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package database

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
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
	return utils.CalculateRelayerKey(
		k.SourceBlockchainID,
		k.DestinationBlockchainID,
		k.OriginSenderAddress,
		k.DestinationAddress,
	)
}
