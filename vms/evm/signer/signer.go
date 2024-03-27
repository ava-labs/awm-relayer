package signer

import (
	"math/big"

	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
)

type Signer interface {
	SignTx(tx *types.Transaction, evmChainID *big.Int) (*types.Transaction, error)
	Address() common.Address
}

func NewSigner(destinationBlockchain *config.DestinationBlockchain) (Signer, error) {
	if destinationBlockchain.AccountPrivateKey == "" {
		return NewKMSSigner(destinationBlockchain.AWSRegion, destinationBlockchain.KMSKeyID)
	}
	return NewTxSigner(destinationBlockchain)
}
