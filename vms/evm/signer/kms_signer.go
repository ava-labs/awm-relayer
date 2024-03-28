// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"bytes"
	"context"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"log"
	"math/big"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var _ Signer = &KMSSigner{}

type KMSSigner struct {
	keyID  string
	client *kms.Client
}

func NewKMSSigner(region, keyID string) (*KMSSigner, error) {
	optFns := []func(*config.LoadOptions) error{
		(func(*config.LoadOptions) error)(config.WithRegion(region)),
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		log.Fatal(err)
	}
	kmsClient := kms.NewFromConfig(awsCfg)
	return &KMSSigner{
		keyID:  keyID,
		client: kmsClient,
	}, nil
}

func (s *KMSSigner) SignTx(tx *types.Transaction, evmChainID *big.Int) (*types.Transaction, error) {
	signer := types.LatestSignerForChainID(evmChainID)
	h := signer.Hash(tx)
	signInput := kms.SignInput{
		KeyId:            aws.String(s.keyID),
		SigningAlgorithm: "ECDSA_SHA_256",
		MessageType:      "DIGEST",
		Message:          h[:],
	}

	// Sign the hash of the transaction via KMS. The returned signature contains the R and S values in ASN.1 format
	signOutput, err := s.client.Sign(context.Background(), &signInput)
	if err != nil {
		return nil, err
	}
	var sigAsn1 struct {
		R asn1.RawValue
		S asn1.RawValue
	}
	_, err = asn1.Unmarshal(signOutput.Signature, &sigAsn1)
	if err != nil {
		return nil, err
	}

	// Retrieve the public key directly from KMS so that we can construct the correct EIP-155 signature
	pubKey, err := s.client.GetPublicKey(context.Background(), &kms.GetPublicKeyInput{
		KeyId: aws.String(s.keyID),
	})
	if err != nil {
		return nil, err
	}

	sigBytes, err := getEthereumSignature(pubKey.PublicKey, h[:], sigAsn1.R.Bytes, sigAsn1.S.Bytes)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(signer, sigBytes)
}

func (s *KMSSigner) Address() common.Address {
	return common.Address{}
}

// Recover the EIP-155 signature from the KMS signature
// KMS returns the signature in ASN.1 format, but the EIP-155 signature is in R || S || V format,
// so we need to test both V = 0 and V = 1 against the recovered public key
//
// This function is adapted from https://github.com/welthee/go-ethereum-aws-kms-tx-signer
// and is reproduced here under welthee's MIT license
func getEthereumSignature(expectedPublicKeyBytes []byte, txHash []byte, r []byte, s []byte) ([]byte, error) {
	rsSignature := append(adjustSignatureLength(r), adjustSignatureLength(s)...)
	signature := append(rsSignature, []byte{0}...)

	recoveredPublicKeyBytes, err := crypto.Ecrecover(txHash, signature)
	if err != nil {
		return nil, err
	}

	if hex.EncodeToString(recoveredPublicKeyBytes) != hex.EncodeToString(expectedPublicKeyBytes) {
		signature = append(rsSignature, []byte{1}...)
		recoveredPublicKeyBytes, err = crypto.Ecrecover(txHash, signature)
		if err != nil {
			return nil, err
		}

		if hex.EncodeToString(recoveredPublicKeyBytes) != hex.EncodeToString(expectedPublicKeyBytes) {
			return nil, errors.New("cannot reconstruct public key from sig")
		}
	}

	return signature, nil
}

// Trim null bytes and pad with zeros to 32 bytes
//
// This function is adapted from https://github.com/welthee/go-ethereum-aws-kms-tx-signer
// and is reproduced here under welthee's MIT license
func adjustSignatureLength(buffer []byte) []byte {
	buffer = bytes.TrimLeft(buffer, "\x00")
	for len(buffer) < 32 {
		zeroBuf := []byte{0}
		buffer = append(zeroBuf, buffer...)
	}
	return buffer
}
