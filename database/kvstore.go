package database

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	errKeyNotFound       = errors.New("key not found")
	errRelayerIDNotFound = errors.New("no database entry for relayer id")
)

const (
	latestProcessedBlockKey DataKey = iota
)

// RelayerDatabase is a key-value store for relayer state, with each relayerID maintaining its own state.
// Implementations should be thread-safe.
type keyValueDatabase interface {
	Get(relayerID common.Hash, key DataKey) ([]byte, error)
	Put(relayerID common.Hash, key DataKey, value []byte) error
}

func NewKeyValueDatabase(logger logging.Logger, cfg *config.Config) (keyValueDatabase, error) {
	relayerIDs := relayer.GetConfigRelayerIDs(cfg)
	dbConnect := func() (keyValueDatabase, error) { return NewJSONFileStorage(logger, cfg.RedisURL, relayerIDs) }
	usedDB := "JSON"
	if cfg.RedisURL != "" {
		dbConnect = func() (keyValueDatabase, error) { return NewRedisDatabase(logger, cfg.StorageLocation, relayerIDs) }
		usedDB = "Redis"
	}
	db, err := dbConnect()
	if err != nil {
		logger.Error(
			fmt.Sprintf("Failed to create %s database", usedDB),
			zap.Error(err),
		)
		return nil, err
	}
	return db, err
}

type DataKey int

func (k DataKey) String() string {
	switch k {
	case latestProcessedBlockKey:
		return "latestProcessedBlock"
	}
	return "unknown"
}
