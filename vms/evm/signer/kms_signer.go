// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"bytes"
	"context"
	"encoding/asn1"
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

type asn1EcPublicKey struct {
	EcPublicKeyInfo asn1EcPublicKeyInfo
	PublicKey       asn1.BitString
}

type asn1EcPublicKeyInfo struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.ObjectIdentifier
}

type asn1EcSig struct {
	R asn1.RawValue
	S asn1.RawValue
}

var _ Signer = &KMSSigner{}

type KMSSigner struct {
	keyID  string
	pubKey []byte
	eoa    common.Address
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

	// Retrieve the public key directly from KMS so that we can construct the correct EIP-155 signature
	kmsPubKey, err := kmsClient.GetPublicKey(context.Background(), &kms.GetPublicKeyInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return nil, err
	}
	var asn1pubk asn1EcPublicKey
	_, err = asn1.Unmarshal(kmsPubKey.PublicKey, &asn1pubk)
	if err != nil {
		return nil, err
	}

	pubKey, err := crypto.UnmarshalPubkey(asn1pubk.PublicKey.Bytes)
	if err != nil {
		return nil, err
	}
	eoa := crypto.PubkeyToAddress(*pubKey)

	return &KMSSigner{
		keyID:  keyID,
		client: kmsClient,
		pubKey: asn1pubk.PublicKey.Bytes,
		eoa:    eoa,
	}, nil
}

func (s *KMSSigner) SignTx(tx *types.Transaction, evmChainID *big.Int) (*types.Transaction, error) {
	signer := types.LatestSignerForChainID(evmChainID)
	h := signer.Hash(tx).Bytes()
	signInput := kms.SignInput{
		KeyId:            aws.String(s.keyID),
		SigningAlgorithm: "ECDSA_SHA_256",
		MessageType:      "DIGEST",
		Message:          h,
	}

	// Sign the hash of the transaction via KMS. The returned signature contains the R and S values in ASN.1 format
	signOutput, err := s.client.Sign(context.Background(), &signInput)
	if err != nil {
		return nil, err
	}
	var sigAsn1 asn1EcSig
	_, err = asn1.Unmarshal(signOutput.Signature, &sigAsn1)
	if err != nil {
		return nil, err
	}

	sigBytes, err := s.getEthereumSignature(h, sigAsn1.R.Bytes, sigAsn1.S.Bytes)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(signer, sigBytes)
}

func (s *KMSSigner) Address() common.Address {
	return s.eoa
}

// Recover the EIP-155 signature from the KMS signature
// KMS returns the signature in ASN.1 format, but the EIP-155 signature is in R || S || V format,
// so we need to test both V = 0 and V = 1 against the recovered public key
//
// This function is adapted from https://github.com/welthee/go-ethereum-aws-kms-tx-signer
// and is reproduced here under welthee's MIT license
func (s *KMSSigner) getEthereumSignature(txHash []byte, rBytes []byte, sBytes []byte) ([]byte, error) {
	rsSignature := append(adjustSignatureLength(rBytes), adjustSignatureLength(sBytes)...)

	// Check if the public key can be reconstructed with v=0
	if signature, err := s.checkRSVSignature(txHash, rsSignature, 0); signature != nil {
		return signature, nil
	} else if err != nil {
		return nil, err
	}

	// Check if the public key can be reconstructed with v=1
	if signature, err := s.checkRSVSignature(txHash, rsSignature, 1); signature != nil {
		return signature, nil
	} else if err != nil {
		return nil, err
	}

	return nil, errors.New("cannot reconstruct public key from sig")
}

// Checks if the public key can be reconstructed from the signature with the given v value
// Returns a non-nil signature if the public key can be reconstructed, nil if it cannot
func (s *KMSSigner) checkRSVSignature(txHash []byte, rsSignature []byte, v uint8) ([]byte, error) {
	signature := append(rsSignature, []byte{v}...)
	pubKey, err := crypto.Ecrecover(txHash, signature)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(pubKey, s.pubKey) {
		return signature, nil
	}
	return nil, nil
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
