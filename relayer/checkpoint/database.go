//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package checkpoint

import (
	"errors"

	"github.com/ava-labs/awm-relayer/relayer"
)

var (
	// Errors expected to be used by the RelayerDatabase implementations
	//
	ErrNotFound                 = errors.New("not found")
	ErrDatabaseMisconfiguration = errors.New("database misconfiguration")
)

// RelayerDatabase defines the interface used by the checkpoint manager to store last processed block height
type RelayerDatabase interface {
	GetLatestProcessedBlockHeight(relayerID relayer.RelayerID) (uint64, error)
	StoreLatestProcessedBlockHeight(relayerID relayer.RelayerID, height uint64) error
}
