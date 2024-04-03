// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"context"
	"strings"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/common"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var _ RelayerDatabase = &RedisDatabase{}

type RedisDatabase struct {
	logger logging.Logger
	client *redis.Client
}

func NewRedisDatabase(logger logging.Logger, redisURL string, relayerIDs []RelayerID) (*RedisDatabase, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Error("Failed to parse Redis URL", zap.Error(err), zap.String("url", redisURL))
		return nil, err
	}

	// Create a new Redis client.
	// The server address, password, db index, and protocol version are extracted from the URL
	// Request timeouts use the default value of 3 seconds
	client := redis.NewClient(opts)
	return &RedisDatabase{
		logger: logger,
		client: client,
	}, nil
}

func (r *RedisDatabase) Get(relayerID common.Hash, key DataKey) ([]byte, error) {
	ctx := context.Background()
	compositeKey := constructCompositeKey(relayerID, key)
	val, err := r.client.Get(ctx, compositeKey).Result()
	if err != nil {
		r.logger.Debug("Error retrieving key from Redis",
			zap.String("key", compositeKey),
			zap.Error(err))
		if err == redis.Nil {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return []byte(val), nil
}

func (r *RedisDatabase) Put(relayerID common.Hash, key DataKey, value []byte) error {
	ctx := context.Background()
	compositeKey := constructCompositeKey(relayerID, key)

	// Persistently store the value in Redis
	err := r.client.Set(ctx, compositeKey, value, 0).Err()
	if err != nil {
		r.logger.Error("Error storing key in Redis",
			zap.String("key", compositeKey),
			zap.Error(err))
		return err
	}
	return nil
}

func constructCompositeKey(relayerID common.Hash, key DataKey) string {
	const keyDelimiter = "-"
	return strings.Join([]string{relayerID.Hex(), key.String()}, keyDelimiter)
}
