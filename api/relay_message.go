package api

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/types"
	relayerTypes "github.com/ava-labs/awm-relayer/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

const (
	RelayAPIPath        = "/relay"
	RelayMessageAPIPath = RelayAPIPath + "/message"
)

type RelayMessageRequest struct {
	// Required. cb58 encoding of the source blockchain ID for the message
	BlockchainID string `json:"blockchain-id"`
	// Required. Hex encoding of the warp message ID
	MessageID string `json:"message-id"`
	// Required. Integer representation of the block number that the message was sent in
	BlockNum string `json:"block-num"`
}

type RelayMessageResponse struct {
	// hex encoding of the source blockchain ID for the message
	TransactionHash string `json:"transaction-hash"`
}

// Defines a manual warp message to be sent from the relayer through the API.
type ManualWarpMessageRequest struct {
	UnsignedMessageBytes []byte
	SourceAddress        common.Address
}

func HandleRelayMessage(messageCoordinator *relayer.MessageCoordinator) {
	http.Handle(RelayAPIPath, relayAPIHandler(messageCoordinator))
}

func HandleRelay(messageCoordinator *relayer.MessageCoordinator) {
	http.Handle(RelayMessageAPIPath, relayMessageAPIHandler(messageCoordinator))
}

func relayMessageAPIHandler(messageCoordinator *relayer.MessageCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ManualWarpMessageRequest

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

		txHash, err := messageCoordinator.ProcessManualWarpMessage(warpMessageInfo)
		if err != nil {
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			http.Error(w, "error writing response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(resp)
	})
}

func relayAPIHandler(messageCoordinator *relayer.MessageCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		txHash, err := messageCoordinator.ProcessMessageID(blockchainID, messageID, blockNum)
		if err != nil {
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			http.Error(w, "error writing response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(resp)
	})
}
