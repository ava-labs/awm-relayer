package cache

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pingcap/errors"
	"go.uber.org/zap"
)

type Cache struct {
	logger     logging.Logger
	signatures *lru.Cache[cacheKey, cacheValue]
}

type cacheKey ids.ID // warp message ID
type cacheValue map[PublicKeyBytes]SignatureBytes

type PublicKeyBytes [bls.PublicKeyLen]byte
type SignatureBytes [bls.SignatureLen]byte

func NewCache(size uint64, logger logging.Logger) (*Cache, error) {
	if size > math.MaxInt {
		return nil, errors.New("cache size too big")
	}

	signatureCache, err := lru.New[cacheKey, cacheValue](int(size))
	if err != nil {
		return nil, err
	}

	return &Cache{
		signatures: signatureCache,
		logger:     logger,
	}, nil
}

func (c *Cache) Get(msgID ids.ID) (cacheValue, bool) {
	cachedValue, isCached := c.signatures.Get(cacheKey(msgID))

	if isCached {
		c.logger.Debug("cache hit", zap.Stringer("msgID", msgID))
		return cachedValue, true
	} else {
		c.logger.Debug("cache miss", zap.Stringer("msgID", msgID))
		return make(cacheValue), false
	}
}

func (c *Cache) Add(
	msgID ids.ID,
	pubKey PublicKeyBytes,
	signature SignatureBytes,
) {
	if signatures, ok := c.Get(msgID); ok {
		signatures[pubKey] = signature
	} else {
		signatures := make(cacheValue)
		signatures[pubKey] = signature
		c.signatures.Add(cacheKey(msgID), signatures)
	}
}
