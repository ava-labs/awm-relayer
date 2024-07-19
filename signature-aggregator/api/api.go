// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/lib/aggregator"
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
	// Required. Unsigned base64 encoded message bytes.
	UnsignedMessageBytes []byte `json:"unsigned-message-bytes"`
	// Optional hex or cb58 encoded signing subnet ID. If omitted will default to the subnetID of the source BlockChain
	SigningSubnetID string `json:"signing-subnet-id"`
	// Optional. Integer from 0 to 100 representing the percentage of the quorum that is required to sign the message
	// defaults to 67 if omitted.
	QuorumNum *uint64 `json:"quorum-num"`
}

type SignatureAggregationResponse struct {
	// base64 encoding of the signature
	SignedMessageBytes []byte `json:"signed-message-bytes"`
}

func HandleSignatureAggregationRawRequest(logger logging.Logger, signatureAggregator *aggregator.SignatureAggregator) {
	http.Handle(RawMessageAPIPath, signatureAggregationAPIHandler(logger, signatureAggregator))
}

func signatureAggregationAPIHandler(logger logging.Logger, signatureAggregator *aggregator.SignatureAggregator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SignatureAggregationRawRequest
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
		var quorumNum uint64
		if req.QuorumNum == nil {
			quorumNum = defaultQuorumNum
		} else if *req.QuorumNum >= 0 || *req.QuorumNum > 100 {
			logger.Warn("Invalid quorum number", zap.Uint64("quorum-num", *req.QuorumNum))
			http.Error(w, "invalid quorum number", http.StatusBadRequest)
			return
		} else {
			quorumNum = *req.QuorumNum
		}
		var signingSubnetID *ids.ID
		if req.SigningSubnetID != "" {
			*signingSubnetID, err = utils.HexOrCB58ToID(req.SigningSubnetID)
			if err != nil {
				logger.Warn("Error parsing signing subnet ID", zap.Error(err))
				http.Error(w, "error parsing signing subnet ID: "+err.Error(), http.StatusBadRequest)
			}
		}

		signedMessage, err := signatureAggregator.AggregateSignaturesAppRequest(unsignedMessage, signingSubnetID, quorumNum)

		if err != nil {
			logger.Warn("Failed to aggregate signatures", zap.Error(err))
			http.Error(w, "error aggregating signatures: "+err.Error(), http.StatusInternalServerError)
		}
		resp, err := json.Marshal(
			SignatureAggregationResponse{
				SignedMessageBytes: signedMessage.Bytes(),
			},
		)

		if err != nil {
			logger.Error("Failed to marshal response", zap.Error(err))
			http.Error(w, "error marshalling a response: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = w.Write(resp)
		if err != nil {
			logger.Error("Error writing response", zap.Error(err))
		}
	})
}
