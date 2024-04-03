// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"runtime"

	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var _ Signer = &TxSigner{}

type TxSigner struct {
	pk  *ecdsa.PrivateKey
	eoa common.Address
}

func NewTxSigner(destinationBlockchain *config.DestinationBlockchain) (*TxSigner, error) {
	pk, err := crypto.HexToECDSA(utils.SanitizeHexString(destinationBlockchain.AccountPrivateKey))
	if err != nil {
		return nil, utils.ErrInvalidPrivateKeyHex
	}

	// Explicitly zero the private key when it is gc'd
	runtime.SetFinalizer(pk, func(pk *ecdsa.PrivateKey) {
		pk.D.SetInt64(0)
		pk = nil
	})

	address := crypto.PubkeyToAddress(pk.PublicKey)
	return &TxSigner{
		pk:  pk,
		eoa: address,
	}, nil
}

func (s *TxSigner) SignTx(tx *types.Transaction, evmChainID *big.Int) (*types.Transaction, error) {
	signer := types.LatestSignerForChainID(evmChainID)
	tx, err := types.SignTx(tx, signer, s.pk)
	if err != nil {
		// Do not return the emitted error, as that function call handles the private key
		return nil, errors.New("failed to sign transaction")
	}
	return tx, nil
}

func (s *TxSigner) Address() common.Address {
	return s.eoa
}
