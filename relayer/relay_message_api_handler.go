package relayer

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

type RelayMessageRequest struct {
	BlockchainID string `json:"blockchain-id"`
	MessageID    string `json:"message-id"`
	BlockNum     string `json:"block-num"`
}

func RelayMessageAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RelayMessageRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		blockchainID, err := ids.FromString(req.BlockchainID)
		if err != nil {
			http.Error(w, "invalid blockchainID"+err.Error(), http.StatusBadRequest)
			return
		}
		messageID := common.HexToHash(req.MessageID)
		blockNum, ok := new(big.Int).SetString(req.BlockNum, 10)
		if !ok {
			http.Error(w, "invalid blockNum", http.StatusBadRequest)
			return
		}

		err = ProcessMessage(blockchainID, messageID, blockNum)
		if err != nil {
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = w.Write([]byte("Message processed successfully"))
	}
}
