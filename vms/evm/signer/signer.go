package signer

import (
	"math/big"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
)

type Signer interface {
	SignTx(tx *types.Transaction, evmChainID *big.Int) (*types.Transaction, error)
	Address() common.Address
}
