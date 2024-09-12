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
	logger logging.Logger

	// map of warp message ID to a map of public keys to signatures
	signatures *lru.Cache[ids.ID, map[PublicKeyBytes]SignatureBytes]
}

type PublicKeyBytes [bls.PublicKeyLen]byte
type SignatureBytes [bls.SignatureLen]byte

func NewCache(size uint64, logger logging.Logger) (*Cache, error) {
	if size > math.MaxInt {
		return nil, errors.New("cache size too big")
	}

	signatureCache, err := lru.New[ids.ID, map[PublicKeyBytes]SignatureBytes](int(size))
	if err != nil {
		return nil, err
	}

	return &Cache{
		signatures: signatureCache,
		logger:     logger,
	}, nil
}

func (c *Cache) Get(msgID ids.ID) (map[PublicKeyBytes]SignatureBytes, bool) {
	cachedValue, isCached := c.signatures.Get(msgID)

	if isCached {
		c.logger.Debug(
			"cache hit",
			zap.Stringer("msgID", msgID),
			zap.Int("signatureCount", len(cachedValue)),
		)
		return cachedValue, true
	} else {
		c.logger.Debug("cache miss", zap.Stringer("msgID", msgID))
		return nil, false
	}
}

func (c *Cache) Add(
	msgID ids.ID,
	pubKey PublicKeyBytes,
	signature SignatureBytes,
) {
	var (
		sigs map[PublicKeyBytes]SignatureBytes
		ok   bool
	)
	if sigs, ok = c.Get(msgID); !ok {
		sigs = make(map[PublicKeyBytes]SignatureBytes)
	}
	sigs[pubKey] = signature
	c.signatures.Add(msgID, sigs)
}
