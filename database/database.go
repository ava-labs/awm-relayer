// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package database

import (
	"github.com/ava-labs/avalanchego/ids"
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
	Get(blockchainID ids.ID, key []byte) ([]byte, error)
	Put(blockchainID ids.ID, key []byte, value []byte) error
}
