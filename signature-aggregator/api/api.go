// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/signature-aggregator/aggregator"
	"github.com/ava-labs/awm-relayer/types"
	"github.com/ava-labs/awm-relayer/utils"
	"go.uber.org/zap"
)

const (
	MessageIDAPIPath  = "/message-id"
	RawMessageAPIPath = "/raw-message"
	defaultQuorumNum  = 67
)

type SignatureAggregationByIDRequest struct {
	// Required. cb58-encoded or "0x" prefixed hex-encoded source subnet ID for the message
	SubnetID string `json:"subnet-id"`
	// Required. cb58-encoded or "0x" prefixed hex-encoded warp message ID
	MessageID string `json:"message-id"`
	// Optional. Integer from 0 to 100 representing the percentage of the quorum that is required to sign the message
	// defaults to 67 if omitted.
	QuorumNum uint64 `json:"quorum-num"`
}

// Defines a request interface for signature aggregation for a raw unsigned message.
// Currently a copy of the `ManualWarpMessageRequest` struct in relay_message.go
type SignatureAggregationRawRequest struct {
	// Required. hex-encoded message, optionally prefixed with "0x".
	UnsignedMessage string `json:"unsigned-message"`
	// Optional hex or cb58 encoded signing subnet ID. If omitted will default to the subnetID of the source BlockChain
	SigningSubnetID string `json:"signing-subnet-id"`
	// Optional. Integer from 0 to 100 representing the percentage of the quorum that is required to sign the message
	// defaults to 67 if omitted.
	QuorumNum uint64 `json:"quorum-num"`
}

type SignatureAggregationResponse struct {
	// hex encoding of the signature
	SignedMessage string `json:"signed-message"`
}

func HandleSignatureAggregationRawRequest(logger logging.Logger, signatureAggregator *aggregator.SignatureAggregator) {
	http.Handle(RawMessageAPIPath, signatureAggregationAPIHandler(logger, signatureAggregator))
}

func writeJsonError(
	logger logging.Logger,
	w http.ResponseWriter,
	errorMsg string,
) {
	resp, err := json.Marshal(struct{ error string }{error: errorMsg})
	if err != nil {
		logger.Error(
			"Error marshalling JSON error response",
			zap.Error(err),
		)
	}

	w.Header().Set("Content-Type", "application/json")

	w.Write(resp)
	if err != nil {
		logger.Error("Error writing error response", zap.Error(err))
	}
}

func signatureAggregationAPIHandler(logger logging.Logger, aggregator *aggregator.SignatureAggregator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SignatureAggregationRawRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			msg := "Could not decode request body"
			logger.Warn(msg, zap.Error(err))
			writeJsonError(logger, w, msg)
			return
		}
		var decodedMessage []byte
		decodedMessage, err = hex.DecodeString(
			strings.TrimPrefix(req.UnsignedMessage, "0x"),
		)
		if err != nil {
			msg := "Could not decode message"
			logger.Warn(
				msg,
				zap.String("msg", req.UnsignedMessage),
				zap.Error(err),
			)
			writeJsonError(logger, w, msg)
			return
		}
		unsignedMessage, err := types.UnpackWarpMessage(decodedMessage)
		if err != nil {
			msg := "Error unpacking warp message"
			logger.Warn(msg, zap.Error(err))
			writeJsonError(logger, w, msg)
			return
		}
		var quorumNum uint64
		if req.QuorumNum == 0 {
			quorumNum = defaultQuorumNum
		} else if req.QuorumNum >= 0 || req.QuorumNum > 100 {
			msg := "Invalid quorum number"
			logger.Warn(msg, zap.Uint64("quorum-num", req.QuorumNum))
			writeJsonError(logger, w, msg)
			return
		} else {
			quorumNum = req.QuorumNum
		}
		var signingSubnetID *ids.ID
		if req.SigningSubnetID != "" {
			*signingSubnetID, err = utils.HexOrCB58ToID(req.SigningSubnetID)
			if err != nil {
				msg := "Error parsing signing subnet ID"
				logger.Warn(
					msg,
					zap.Error(err),
					zap.String("input", req.SigningSubnetID),
				)
				writeJsonError(logger, w, msg)
			}
		}

		signedMessage, err := aggregator.AggregateSignaturesAppRequest(unsignedMessage, signingSubnetID, quorumNum)
		if err != nil {
			msg := "Failed to aggregate signatures"
			logger.Warn(msg, zap.Error(err))
			writeJsonError(logger, w, msg)
		}
		resp, err := json.Marshal(
			SignatureAggregationResponse{
				SignedMessage: hex.EncodeToString(
					signedMessage.Bytes(),
				),
			},
		)

		if err != nil {
			msg := "Failed to marshal response"
			logger.Error(msg, zap.Error(err))
			writeJsonError(logger, w, msg)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(resp)
		if err != nil {
			logger.Error("Error writing response", zap.Error(err))
		}
	})
}
