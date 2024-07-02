package api

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/relayer"
	"github.com/ava-labs/awm-relayer/types"
	relayerTypes "github.com/ava-labs/awm-relayer/types"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const (
	RelayAPIPath        = "/relay"
	RelayMessageAPIPath = RelayAPIPath + "/message"
)

type RelayMessageRequest struct {
	// Required. cb58 encoding of the source blockchain ID for the message
	BlockchainID string `json:"blockchain-id"`
	// Required. cb58 encoding of the warp message ID
	MessageID string `json:"message-id"`
	// Required. Block number that the message was sent in
	BlockNum uint64 `json:"block-num"`
}

type RelayMessageResponse struct {
	// hex encoding of the source blockchain ID for the message
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
			logger.Warn("could not decode request body")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		unsignedMessage, err := types.UnpackWarpMessage(req.UnsignedMessageBytes)
		if err != nil {
			logger.Warn("error unpacking warp message", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		warpMessageInfo := &relayerTypes.WarpMessageInfo{
			SourceAddress:   common.HexToAddress(req.SourceAddress),
			UnsignedMessage: unsignedMessage,
		}

		txHash, err := messageCoordinator.ProcessWarpMessage(warpMessageInfo)
		if err != nil {
			logger.Error("error processing message", zap.Error(err))
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			logger.Error("error marshaling response", zap.Error(err))
			http.Error(w, "error marshaling response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(resp)
		if err != nil {
			logger.Error("error writing response", zap.Error(err))
		}
	})
}

func relayAPIHandler(logger logging.Logger, messageCoordinator *relayer.MessageCoordinator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RelayMessageRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			logger.Warn("could not decode request body")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		blockchainID, err := ids.FromString(req.BlockchainID)
		if err != nil {
			logger.Warn("invalid blockchainID", zap.String("blockchainID", req.BlockchainID))
			http.Error(w, "invalid blockchainID: "+err.Error(), http.StatusBadRequest)
			return
		}
		messageID, err := ids.FromString(req.MessageID)
		if err != nil {
			logger.Warn("invalid messageID", zap.String("messageID", req.MessageID))
			http.Error(w, "invalid messageID: "+err.Error(), http.StatusBadRequest)
			return
		}

		txHash, err := messageCoordinator.ProcessMessageID(blockchainID, messageID, new(big.Int).SetUint64(req.BlockNum))
		if err != nil {
			logger.Error("error processing message", zap.Error(err))
			http.Error(w, "error processing message: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := json.Marshal(
			RelayMessageResponse{
				TransactionHash: txHash.Hex(),
			},
		)
		if err != nil {
			logger.Error("error marshalling response", zap.Error(err))
			http.Error(w, "error marshalling response: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(resp)
		if err != nil {
			logger.Error("error writing response", zap.Error(err))
		}
	})
}
