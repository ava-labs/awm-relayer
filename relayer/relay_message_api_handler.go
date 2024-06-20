package relayer

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/ava-labs/awm-relayer/types"
	relayerTypes "github.com/ava-labs/awm-relayer/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

const (
	RelayApiPath        = "/relay"
	RelayMessageApiPath = RelayApiPath + "/message"
)

type RelayMessageRequest struct {
	// Required. cb58 encoding of the blockchain ID
	BlockchainID string `json:"blockchain-id"`
	// Required. Hex encoding of the warp message ID
	MessageID string `json:"message-id"`
	// Required. Integer representation of the block number
	BlockNum string `json:"block-num"`
}

// Defines a manual warp message to be sent from the relayer through the API.
type ManualWarpMessage struct {
	UnsignedMessageBytes    []byte
	SourceBlockchainID      ids.ID
	DestinationBlockchainID ids.ID
	SourceAddress           common.Address
	DestinationAddress      common.Address
}

func RelayMessageAPIHandler(w http.ResponseWriter, r *http.Request) {
	var req ManualWarpMessage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	unsignedMessage, err := types.UnpackWarpMessage(req.UnsignedMessageBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	warpMessageInfo := &relayerTypes.WarpMessageInfo{
		SourceAddress:   req.SourceAddress,
		UnsignedMessage: unsignedMessage,
	}

	txHash, err := ProcessManualWarpMessage(warpMessageInfo)
	if err != nil {
		http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = w.Write([]byte("Message processed successfully. Transaction Hash: " + txHash.Hex()))
}

func RelayAPIHandler(w http.ResponseWriter, r *http.Request) {
	var req RelayMessageRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	blockchainID, err := ids.FromString(req.BlockchainID)
	if err != nil {
		http.Error(w, "invalid blockchainID: "+err.Error(), http.StatusBadRequest)
		return
	}
	messageID := common.HexToHash(req.MessageID)
	blockNum, ok := new(big.Int).SetString(req.BlockNum, 10)
	if !ok {
		http.Error(w, "invalid blockNum", http.StatusBadRequest)
		return
	}

	txHash, err := ProcessMessage(blockchainID, messageID, blockNum)
	if err != nil {
		http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = w.Write([]byte("Message processed successfully. Transaction Hash: " + txHash.Hex()))
}
