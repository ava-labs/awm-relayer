// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:generate mockgen -source=$GOFILE -destination=./mocks/mock_database.go -package=mocks

package database

import (
	"strconv"

	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/relayer/checkpoint"
	"github.com/pkg/errors"
)

var _ checkpoint.RelayerDatabase = &relayerDatabase{}

// NewRelayerDatabase instantiate and return a relayerDatabase
func NewRelayerDatabase(db keyValueDatabase) *relayerDatabase {
	return &relayerDatabase{
		keyValueDatabase: db,
	}
}

// relayerDatabase implements the checkpoint RelayerDatabase interface
type relayerDatabase struct {
	keyValueDatabase
}

func (x *relayerDatabase) GetLatestProcessedBlockHeight(relayerID relayer.RelayerID) (uint64, error) {
	latestProcessedBlockData, err := x.Get(relayerID.ID, latestProcessedBlockKey)
	if err != nil {
		if isKeyNotFoundError(err) {
			return 0, checkpoint.ErrNotFound
		}
		return 0, err
	}
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		return 0, err
	}
	return latestProcessedBlock, nil
}

func (x *relayerDatabase) StoreLatestProcessedBlockHeight(relayerID relayer.RelayerID, height uint64) error {
	return x.Put(
		relayerID.ID,
		latestProcessedBlockKey,
		[]byte(strconv.FormatUint(height, 10)),
	)
}

// Returns true if an error returned by a RelayerDatabase indicates the requested key was not found.
func isKeyNotFoundError(err error) bool {
	return errors.Is(err, errRelayerIDNotFound) || errors.Is(err, errKeyNotFound)
}
