package api

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	RelayAPIPath        = "/relay"
	RelayMessageAPIPath = RelayAPIPath + "/message"
)

type RelayMessageRequest struct {
	// Required. cb58-encoded or "0x" prefixed hex-encoded source blockchain ID for the message
	BlockchainID string `json:"blockchain-id"`
	// Required. cb58-encoded or "0x" prefixed hex-encoded warp message ID
	MessageID string `json:"message-id"`
	// Required. Block number that the message was sent in
	BlockNum uint64 `json:"block-num"`
}

type RelayMessageResponse struct {
	// hex encoding of the transaction hash containing the processed message
	TransactionHash string `json:"transaction-hash"`
}

// Defines a manual warp message to be sent from the relayer through the API.
type ManualWarpMessageRequest struct {
	UnsignedMessageBytes []byte `json:"unsigned-message-bytes"`
	SourceAddress        string `json:"source-address"`
}

func HandleRelayMessage(logger logging.Logger, messageCoordinator *relayer.MessageCoordinator) {
	http.Handle(RelayAPIPath, relayAPIHandler(logger, messageCoordinator))
}

func HandleRelay(logger logging.Logger, messageCoordinator *relayer.MessageCoordinator) {
	http.Handle(RelayMessageAPIPath, relayMessageAPIHandler(logger, messageCoordinator))
}

func relayMessageAPIHandler(logger logging.Logger, messageCoordinator *relayer.MessageCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ManualWarpMessageRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			logger.Warn("Could not decode request body")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		unsignedMessage, err := types.UnpackWarpMessage(req.UnsignedMessageBytes)
		if err != nil {
			logger.Warn("Error unpacking warp message", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		warpMessageInfo := &types.WarpMessageInfo{
			SourceAddress:   common.HexToAddress(req.SourceAddress),
			UnsignedMessage: unsignedMessage,
		}

		txHash, err := messageCoordinator.ProcessWarpMessage(warpMessageInfo)
		if err != nil {
			logger.Error("Error processing message", zap.Error(err))
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			logger.Error("Error marshaling response", zap.Error(err))
			http.Error(w, "error marshaling response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(resp)
		if err != nil {
			logger.Error("Error writing response", zap.Error(err))
		}
	})
}

func relayAPIHandler(logger logging.Logger, messageCoordinator *relayer.MessageCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RelayMessageRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			logger.Warn("Could not decode request body")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		blockchainID, err := utils.HexOrCB58ToID(req.BlockchainID)
		if err != nil {
			logger.Warn("Invalid blockchainID", zap.String("blockchainID", req.BlockchainID))
			http.Error(w, "invalid blockchainID: "+err.Error(), http.StatusBadRequest)
			return
		}
		messageID, err := utils.HexOrCB58ToID(req.MessageID)
		if err != nil {
			logger.Warn("Invalid messageID", zap.String("messageID", req.MessageID))
			http.Error(w, "invalid messageID: "+err.Error(), http.StatusBadRequest)
			return
		}

		txHash, err := messageCoordinator.ProcessMessageID(blockchainID, messageID, new(big.Int).SetUint64(req.BlockNum))
		if err != nil {
			logger.Error("Error processing message", zap.Error(err))
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			logger.Error("Error marshalling response", zap.Error(err))
			http.Error(w, "error marshalling response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(resp)
		if err != nil {
			logger.Error("Error writing response", zap.Error(err))
		}
	})
}
