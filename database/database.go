// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package database

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	ErrKeyNotFound              = errors.New("key not found")
	ErrRelayerIDNotFound        = errors.New("no database entry for relayer id")
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

// RelayerDatabase is a key-value store for relayer state, with each relayerID maintaining its own state.
// Implementations should be thread-safe.
type RelayerDatabase interface {
	Get(relayerID common.Hash, key DataKey) ([]byte, error)
	Put(relayerID common.Hash, key DataKey, value []byte) error
}

func NewDatabase(logger logging.Logger, cfg *config.Config) (RelayerDatabase, error) {
	if cfg.RedisURL != "" {
		db, err := NewRedisDatabase(logger, cfg.RedisURL, GetConfigRelayerIDs(cfg))
		if err != nil {
			logger.Error(
				"Failed to create Redis database",
				zap.Error(err),
			)
			return nil, err
		}
		return db, nil
	} else {
		db, err := NewJSONFileStorage(logger, cfg.StorageLocation, GetConfigRelayerIDs(cfg))
		if err != nil {
			logger.Error(
				"Failed to create JSON database",
				zap.Error(err),
			)
			return nil, err
		}
		return db, nil
	}
}
