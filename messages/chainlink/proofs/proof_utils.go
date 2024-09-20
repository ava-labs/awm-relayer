// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proofs

import (
	"context"
	"encoding/hex"
	"fmt"

	corethtypes "github.com/ava-labs/coreth/core/types"
	corethethclient "github.com/ava-labs/coreth/ethclient"
	corethtrie "github.com/ava-labs/coreth/trie"
	subnetevmtypes "github.com/ava-labs/subnet-evm/core/types"
	subnetevmethclient "github.com/ava-labs/subnet-evm/ethclient"
	subnetevmtrie "github.com/ava-labs/subnet-evm/trie"
	subnetevmtriedb "github.com/ava-labs/subnet-evm/triedb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// TODO: Is there a better way to handle the different block hash constructions between coreth and subnet-evm
// that doesn't require duplicating the ConstructReceiptProof function?

// Constructs a Merkle proof for the specified txIndex in the block with the given blockHash.
// The proof is returned as a memorydb.Database.
// Should only be used for chains that use coreth as their VM. Not compatible with subnet-evm chains.
func ConstructCorethReceiptProof(
	ctx context.Context,
	ethClient corethethclient.Client,
	blockHash common.Hash,
	txIndex uint,
) (*memorydb.Database, error) {
	// Get the block info
	blockInfo, err := ethClient.BlockByHash(ctx, blockHash)
	if err != nil || blockInfo == nil {
		log.Error("Failed to get block info", "blockHash", blockHash.String(), "err", err)
		return nil, err
	}
	if blockInfo.Hash() != blockHash {
		log.Error("Block hash does not match", "blockHash", blockHash.String())
		return nil, fmt.Errorf("block hash does not match")
	}

	encodedBlockHeader, err := rlp.EncodeToBytes(blockInfo.Header())
	if err != nil {
		log.Error("Failed to encode block header", "blockHash", blockHash.String(), "err", err)
		return nil, err
	}
	log.Info("Fetched block header", "blockHash", blockHash.String(), "header", hex.EncodeToString(encodedBlockHeader))

	// Get the receipts for each transaction in the block
	receipts := make([]*corethtypes.Receipt, blockInfo.Transactions().Len())
	for i, tx := range blockInfo.Transactions() {
		receipt, err := ethClient.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			log.Error("Failed to get transaction receipt", "txHash", tx.Hash().String(), "err", err)
			return nil, err
		}
		receipts[i] = receipt
		encodedReceipt, err := rlp.EncodeToBytes(receipt)
		if err != nil {
			log.Error("Failed to encode receipt", "txHash", tx.Hash().String(), "err", err)
			return nil, err
		}
		log.Info("Fetched encoded receipt", "txHash", tx.Hash().String(), "receipt", hex.EncodeToString(encodedReceipt))
	}
	log.Info("Fetched all receipts for block", "blockHash", blockHash.String(), "numReceipts", len(receipts))

	// Create a trie of the receipts
	receiptTrie, err := corethtrie.New(corethtrie.StateTrieID(common.Hash{}), corethtrie.NewDatabase(nil, nil))
	if err != nil {
		log.Error("Failed to create receipt trie", "err", err)
		return nil, err
	}

	// Defensive check that the receipts root matches the block header.
	// This should always be the case.
	receiptsRoot := corethtypes.DeriveSha(corethtypes.Receipts(receipts), receiptTrie)
	log.Info("Computed receipts trie root", "root", receiptsRoot.String())
	if receiptsRoot != blockInfo.Header().ReceiptHash {
		log.Error("Receipts root does not match", "blockHash", blockHash.String())
		return nil, err
	}

	// Construct the proof of the request receipt against the trie.
	key, err := rlp.EncodeToBytes(txIndex)
	if err != nil {
		log.Error("Failed to encode tx index", "err", err)
		return nil, err
	}
	memoryDB := memorydb.New()
	err = receiptTrie.Prove(key, memoryDB)
	if err != nil {
		log.Error("Failed to prove receipt", "err", err)
		return nil, err
	}
	log.Info("Created Merkle proof for receipt", "txIndex", txIndex)

	// Double check that the proof is valid.
	verifiedValue, err := corethtrie.VerifyProof(receiptsRoot, key, memoryDB)
	if err != nil {
		log.Error("Failed to verify proof", "err", err)
		return nil, err
	}
	log.Info("Verified proof", "key", hex.EncodeToString(key), "value", hex.EncodeToString(verifiedValue))

	return memoryDB, nil
}

// Constructs a Merkle proof for the specified txIndex in the block with the given blockHash.
// The proof is returned as a memorydb.Database.
// Should only be used for chains that use subnet-evm as their VM. Not compatible with coreth chains.
func ConstructSubnetEVMReceiptProof(
	ctx context.Context,
	ethClient subnetevmethclient.Client,
	blockHash common.Hash,
	txIndex uint,
) (*memorydb.Database, error) {
	// Get the block info
	blockInfo, err := ethClient.BlockByHash(ctx, blockHash)
	if err != nil || blockInfo == nil {
		log.Error("Failed to get block info", "blockHash", blockHash.String(), "err", err)
		return nil, err
	}
	if blockInfo.Hash() != blockHash {
		log.Error("Block hash does not match", "blockHash", blockHash.String())
		return nil, fmt.Errorf("block hash does not match")
	}

	// Get the receipts for each transaction in the block
	receipts := make([]*subnetevmtypes.Receipt, blockInfo.Transactions().Len())
	for i, tx := range blockInfo.Transactions() {
		receipt, err := ethClient.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			log.Error("Failed to get transaction receipt", "txHash", tx.Hash().String(), "err", err)
			return nil, err
		}
		receipts[i] = receipt
		encodedReceipt, err := rlp.EncodeToBytes(receipt)
		if err != nil {
			log.Error("Failed to encode receipt", "txHash", tx.Hash().String(), "err", err)
			return nil, err
		}
		log.Info("Got encoded receipt", "txHash", tx.Hash().String(), "receipt", hex.EncodeToString(encodedReceipt))
	}

	// Create a trie of the receipts
	receiptTrie, err := subnetevmtrie.New(subnetevmtrie.StateTrieID(common.Hash{}), subnetevmtriedb.NewDatabase(nil, nil))
	if err != nil {
		log.Error("Failed to create receipt trie", "err", err)
		return nil, err
	}

	// Defensive check that the receipts root matches the block header.
	// This should always be the case.
	receiptsRoot := subnetevmtypes.DeriveSha(subnetevmtypes.Receipts(receipts), receiptTrie)
	if receiptsRoot != blockInfo.Header().ReceiptHash {
		log.Error("Receipts root does not match", "blockHash", blockHash.String())
		return nil, err
	}

	// Construct the proof of the request receipt against the trie.
	key, err := rlp.EncodeToBytes(txIndex)
	if err != nil {
		log.Error("Failed to encode tx index", "err", err)
		return nil, err
	}
	memoryDB := memorydb.New()
	err = receiptTrie.Prove(key, memoryDB)
	if err != nil {
		log.Error("Failed to prove receipt", "err", err)
		return nil, err
	}

	// Double check that the proof is valid.
	verifiedValue, err := subnetevmtrie.VerifyProof(receiptsRoot, key, memoryDB)
	if err != nil {
		log.Error("Failed to verify proof", "err", err)
		return nil, err
	}
	log.Info("Verified proof", "value", hex.EncodeToString(verifiedValue))

	return memoryDB, nil
}

func EncodeMerkleProof(proofDB *memorydb.Database) [][]byte {
	encodedProof := make([][]byte, 0)
	it := proofDB.NewIterator(nil, nil)
	for it.Next() {
		encodedProof = append(encodedProof, it.Value())
	}
	return encodedProof
}
