// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package eventimporter

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

// PackImportEvent packs input to form a call to the importEvent function
func PackImportEvent(
	blockHeader []byte,
	txIndex *big.Int,
	receiptProof [][]byte,
	logIndex *big.Int,
) ([]byte, error) {
	abi, err := EventImporterMetaData.GetAbi()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get abi")
	}

	return abi.Pack("importEvent", common.Hash{}, blockHeader, txIndex, receiptProof, logIndex)
}
