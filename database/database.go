package database

import "github.com/ava-labs/avalanchego/ids"

const (
	LatestProcessedBlockKey = "latestProcessedBlock"
)

// RelayerDatabase is a key-value store for relayer state, with each chainID maintaining its own state
type RelayerDatabase interface {
	Get(chainID ids.ID, key []byte) ([]byte, error)
	Put(chainID ids.ID, key []byte, value []byte) error
}
