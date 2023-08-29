package database

import "github.com/ava-labs/avalanchego/ids"

const (
	LatestBlockHeightKey = "latestBlockHeight"
)

type RelayerDatabase interface {
	Get(chainID ids.ID, key []byte) ([]byte, error)
	Put(chainID ids.ID, key []byte, value []byte) error
}
