// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/utils"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
	"go.uber.org/zap"
)

type blsSignatureBuf [bls.SignatureLen]byte

const (
	// Number of retries to collect signatures from validators
	maxRelayerQueryAttempts = 5
	// Maximum amount of time to spend waiting (in addition to network round trip time per attempt) during relayer signature query routine
	signatureRequestRetryWaitPeriodMs = 10_000
)

var (
	codec        = msg.Codec
	coreEthCodec = coreEthMsg.Codec
	// Errors
	errNotEnoughSignatures = errors.New("failed to collect a threshold of signatures")
	errFailedToGetAggSig   = errors.New("failed to get aggregate signature from node endpoint")
)

// messageRelayers are created for each warp message to be relayed.
// They collect signatures from validators, aggregate them,
// and send the signed warp message to the destination chain.
// Each messageRelayer runs in its own goroutine.
type messageRelayer struct {
	relayer                 *Relayer
	warpMessage             *avalancheWarp.UnsignedMessage
	destinationBlockchainID ids.ID
	warpQuorum              config.WarpQuorum
}

func newMessageRelayer(
	relayer *Relayer,
	warpMessage *avalancheWarp.UnsignedMessage,
	destinationBlockchainID ids.ID,
) (*messageRelayer, error) {
	quorum, err := relayer.globalConfig.GetWarpQuorum(destinationBlockchainID)
	if err != nil {
		relayer.logger.Error(
			"Failed to get warp quorum from config. Relayer may not be configured to deliver to the destination chain.",
			zap.Error(err),
			zap.String("destinationBlockchainID", destinationBlockchainID.String()),
		)
		return nil, err
	}
	return &messageRelayer{
		relayer:                 relayer,
		warpMessage:             warpMessage,
		destinationBlockchainID: destinationBlockchainID,
		warpQuorum:              quorum,
	}, nil
}

func (r *messageRelayer) relayMessage(unsignedMessage *avalancheWarp.UnsignedMessage, requestID uint32, messageManager messages.MessageManager, useAppRequestNetwork bool) error {
	shouldSend, err := messageManager.ShouldSendMessage(unsignedMessage, r.destinationBlockchainID)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to check if message should be sent",
			zap.Error(err),
		)

		r.incFailedRelayMessageCount("failed to check if message should be sent")

		return err
	}
	if !shouldSend {
		r.relayer.logger.Info("Message should not be sent")
		return nil
	}

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	var signedMessage *avalancheWarp.Message
	if useAppRequestNetwork {
		signedMessage, err = r.createSignedMessageAppRequest(requestID)
		if err != nil {
			r.relayer.logger.Error(
				"Failed to create signed warp message via AppRequest network",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via AppRequest network")
			return err
		}
	} else {
		signedMessage, err = r.createSignedMessage()
		if err != nil {
			r.relayer.logger.Error(
				"Failed to create signed warp message via RPC",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via RPC")
			return err
		}
	}

	// create signed message latency (ms)
	r.setCreateSignedMessageLatencyMS(float64(time.Since(startCreateSignedMessageTime).Milliseconds()))

	err = messageManager.SendMessage(signedMessage, r.destinationBlockchainID)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to send warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to send warp message")
		return err
	}
	r.relayer.logger.Info(
		"Finished relaying message to destination chain",
		zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
	)
	r.incSuccessfulRelayMessageCount()
	return nil
}

// createSignedMessage fetches the signed Warp message from the source chain via RPC.
// Each VM may implement their own RPC method to construct the aggregate signature, which
// will need to be accounted for here.
func (r *messageRelayer) createSignedMessage() (*avalancheWarp.Message, error) {
	r.relayer.logger.Info("Fetching aggregate signature from the source chain validators via API")
	warpClient, err := warpBackend.NewClient(r.relayer.apiNodeURI, r.relayer.sourceBlockchainID.String())
	if err != nil {
		r.relayer.logger.Error(
			"Failed to create Warp API client",
			zap.Error(err),
		)
		return nil, err
	}
	signingSubnetID := r.relayer.sourceSubnetID
	if r.relayer.sourceSubnetID == constants.PrimaryNetworkID {
		signingSubnetID, err = r.relayer.pChainClient.ValidatedBy(context.Background(), r.destinationBlockchainID)
		if err != nil {
			r.relayer.logger.Error(
				"failed to get validating subnet for destination chain",
				zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
				zap.Error(err),
			)
			return nil, err
		}
	}

	var signedWarpMessageBytes []byte
	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		r.relayer.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", r.relayer.sourceBlockchainID.String()),
			zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
			zap.String("signingSubnetID", signingSubnetID.String()),
		)
		signedWarpMessageBytes, err = warpClient.GetMessageAggregateSignature(
			context.Background(),
			r.warpMessage.ID(),
			r.warpQuorum.QuorumNumerator,
			signingSubnetID.String(),
		)
		if err == nil {
			warpMsg, err := avalancheWarp.ParseMessage(signedWarpMessageBytes)
			if err != nil {
				r.relayer.logger.Error(
					"Failed to parse signed warp message",
					zap.Error(err),
				)
				return nil, err
			}
			return warpMsg, err
		}
		r.relayer.logger.Info(
			"Failed to get aggregate signature from node endpoint. Retrying.",
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
		if attempt != maxRelayerQueryAttempts {
			// Sleep such that all retries are uniformly spread across totalRelayerQueryPeriodMs
			// TODO: We may want to consider an exponential back off rather than a uniform sleep period.
			time.Sleep(time.Duration(signatureRequestRetryWaitPeriodMs/maxRelayerQueryAttempts) * time.Millisecond)
		}
	}
	r.relayer.logger.Warn(
		"Failed to get aggregate signature from node endpoint",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("sourceBlockchainID", r.relayer.sourceBlockchainID.String()),
		zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
		zap.String("signingSubnetID", signingSubnetID.String()),
	)
	return nil, errFailedToGetAggSig
}

// createSignedMessageAppRequest collects signatures from nodes by directly querying them via AppRequest, then aggregates the signatures, and constructs the signed warp message.
func (r *messageRelayer) createSignedMessageAppRequest(requestID uint32) (*avalancheWarp.Message, error) {
	r.relayer.logger.Info("Fetching aggregate signature from the source chain validators via AppRequest")

	// Get the current canonical validator set of the source subnet.
	validatorSet, totalValidatorWeight, err := r.getCurrentCanonicalValidatorSet()
	if err != nil {
		r.relayer.logger.Error(
			"Failed to get the canonical subnet validator set",
			zap.String("subnetID", r.relayer.sourceSubnetID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	// We make queries to node IDs, not unique validators as represented by a BLS pubkey, so we need this map to track
	// responses from nodes and populate the signatureMap with the corresponding validator signature
	// This maps node IDs to the index in the canonical validator set
	nodeValidatorIndexMap := make(map[ids.NodeID]int)
	for i, vdr := range validatorSet {
		for _, node := range vdr.NodeIDs {
			nodeValidatorIndexMap[node] = i
		}
	}

	// Manually connect to all peers in the validator set
	// If new peers are connected, AppRequests may fail while the handshake is in progress.
	// In that case, AppRequests to those nodes will be retried in the next iteration of the retry loop.
	nodeIDs := set.NewSet[ids.NodeID](len(nodeValidatorIndexMap))
	for node := range nodeValidatorIndexMap {
		nodeIDs.Add(node)
	}

	// TODO: We may still be able to proceed with signature aggregation even if we fail to connect to some peers.
	// 		 We should check if the connected set represents sufficient stake, and continue if so.
	_, err = r.relayer.network.ConnectPeers(nodeIDs)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to connect to peers",
			zap.Error(err),
		)
		return nil, err
	}

	// Construct the request

	// Make sure to use the correct codec
	var reqBytes []byte
	if r.relayer.sourceSubnetID == constants.PrimaryNetworkID {
		req := coreEthMsg.MessageSignatureRequest{
			MessageID: r.warpMessage.ID(),
		}
		reqBytes, err = coreEthMsg.RequestToBytes(coreEthCodec, req)
	} else {
		req := msg.MessageSignatureRequest{
			MessageID: r.warpMessage.ID(),
		}
		reqBytes, err = msg.RequestToBytes(codec, req)
	}
	if err != nil {
		r.relayer.logger.Error(
			"Failed to marshal request bytes",
			zap.String("warpMessageID", r.warpMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}

	outMsg, err := r.relayer.messageCreator.AppRequest(r.warpMessage.SourceChainID, requestID, peers.DefaultAppRequestTimeout, reqBytes)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to create app request message",
			zap.Error(err),
		)
		return nil, err
	}

	// Query the validators with retries. On each retry, query one node per unique BLS pubkey
	accumulatedSignatureWeight := big.NewInt(0)

	signatureMap := make(map[int]blsSignatureBuf)

	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		responsesExpected := len(validatorSet) - len(signatureMap)
		r.relayer.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
			zap.Int("validatorSetSize", len(validatorSet)),
			zap.Int("signatureMapSize", len(signatureMap)),
			zap.Int("responsesExpected", responsesExpected),
		)

		vdrSet := set.NewSet[ids.NodeID](len(validatorSet))
		for i, vdr := range validatorSet {
			// If we already have the signature for this validator, do not query any of the composite nodes again
			if _, ok := signatureMap[i]; ok {
				continue
			}

			// TODO: Track failures and iterate through the validator's node list on subsequent query attempts
			nodeID := vdr.NodeIDs[0]
			vdrSet.Add(nodeID)
			r.relayer.logger.Debug(
				"Added node ID to query.",
				zap.String("nodeID", nodeID.String()),
			)

			// Register a timeout response for each queried node
			reqID := ids.RequestID{
				NodeID:             nodeID,
				SourceChainID:      r.warpMessage.SourceChainID,
				DestinationChainID: r.warpMessage.SourceChainID,
				RequestID:          requestID,
				Op:                 byte(message.AppResponseOp),
			}
			r.relayer.network.Handler.RegisterRequest(reqID)
		}

		sentTo := r.relayer.network.Network.Send(outMsg, vdrSet, r.relayer.sourceSubnetID, subnets.NoOpAllower)
		r.relayer.logger.Debug(
			"Sent signature request to network",
			zap.String("messageID", r.warpMessage.ID().String()),
			zap.Any("sentTo", sentTo),
		)
		for nodeID := range vdrSet {
			if !sentTo.Contains(nodeID) {
				r.relayer.logger.Warn(
					"Failed to make async request to node",
					zap.String("nodeID", nodeID.String()),
					zap.Error(err),
				)
				responsesExpected--
			}
		}

		responseCount := 0
		if responsesExpected > 0 {
			// Handle the responses. For each response, we need to call response.OnFinishedHandling() exactly once.
			// Wrap the loop body in an anonymous function so that we do so on each loop iteration
			for response := range r.relayer.responseChan {
				r.relayer.logger.Debug(
					"Processing response from node",
					zap.String("nodeID", response.NodeID().String()),
				)
				// This anonymous function attempts to create a signed warp message from the accumulated responses
				// Returns an error only if a non-recoverable error occurs, otherwise returns (nil, nil) to continue processing responses
				// When a non-nil signedMsg is returned, createSignedMessage itself returns
				signedMsg, err := func() (*avalancheWarp.Message, error) {
					defer response.OnFinishedHandling()

					// Check if this is an expected response.
					m := response.Message()
					rcvReqID, ok := message.GetRequestID(m)
					if !ok {
						// This should never occur, since inbound message validity is already checked by the inbound handler
						r.relayer.logger.Error("Could not get requestID from message")
						return nil, nil
					}
					nodeID := response.NodeID()
					if !sentTo.Contains(nodeID) || rcvReqID != requestID {
						r.relayer.logger.Debug("Skipping irrelevant app response")
						return nil, nil
					}

					// Count the relevant app message
					responseCount++

					// If we receive an AppRequestFailed, then the request timed out.
					// We still want to increment responseCount, since we are no longer expecting a response from that node.
					if response.Op() == message.AppErrorOp {
						r.relayer.logger.Debug("Request timed out")
						return nil, nil
					}

					validator := validatorSet[nodeValidatorIndexMap[nodeID]]
					signature, valid := r.isValidSignatureResponse(response, validator.PublicKey)
					if valid {
						r.relayer.logger.Debug(
							"Got valid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						signatureMap[nodeValidatorIndexMap[nodeID]] = signature
						accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))
					} else {
						r.relayer.logger.Debug(
							"Got invalid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						return nil, nil
					}

					// As soon as the signatures exceed the stake weight threshold we try to aggregate and send the transaction.
					if utils.CheckStakeWeightExceedsThreshold(
						accumulatedSignatureWeight,
						totalValidatorWeight,
						r.warpQuorum.QuorumNumerator,
						r.warpQuorum.QuorumDenominator,
					) {
						aggSig, vdrBitSet, err := r.aggregateSignatures(signatureMap)
						if err != nil {
							r.relayer.logger.Error(
								"Failed to aggregate signature.",
								zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
								zap.Error(err),
							)
							return nil, err
						}

						signedMsg, err := avalancheWarp.NewMessage(r.warpMessage, &avalancheWarp.BitSetSignature{
							Signers:   vdrBitSet.Bytes(),
							Signature: *(*[bls.SignatureLen]byte)(bls.SignatureToBytes(aggSig)),
						})
						if err != nil {
							r.relayer.logger.Error(
								"Failed to create new signed message",
								zap.Error(err),
							)
							return nil, err
						}
						return signedMsg, nil
					}
					// Not enough signatures, continue processing messages
					return nil, nil
				}()
				if err != nil {
					return nil, err
				}
				// If we have sufficient signatures, return here.
				if signedMsg != nil {
					r.relayer.logger.Info(
						"Created signed message.",
						zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
					)
					return signedMsg, nil
				}
				// Break once we've had successful or unsuccessful responses from each requested node
				if responseCount == responsesExpected {
					break
				}
			}
		}
		if attempt != maxRelayerQueryAttempts {
			// Sleep such that all retries are uniformly spread across totalRelayerQueryPeriodMs
			// TODO: We may want to consider an exponential back off rather than a uniform sleep period.
			time.Sleep(time.Duration(signatureRequestRetryWaitPeriodMs/maxRelayerQueryAttempts) * time.Millisecond)
		}
	}

	r.relayer.logger.Warn(
		"Failed to collect a threshold of signatures",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
	)
	return nil, errNotEnoughSignatures
}

func (r *messageRelayer) getCurrentCanonicalValidatorSet() ([]*avalancheWarp.Validator, uint64, error) {
	var (
		signingSubnet ids.ID
		err           error
	)
	if r.relayer.sourceSubnetID == constants.PrimaryNetworkID {
		// If the message originates from the primary subnet, then we instead "self sign" the message using the validators of the destination subnet.
		signingSubnet, err = r.relayer.pChainClient.ValidatedBy(context.Background(), r.destinationBlockchainID)
		if err != nil {
			r.relayer.logger.Error(
				"Failed to get validating subnet for destination chain",
				zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
				zap.Error(err),
			)
			return nil, 0, err
		}
	} else {
		// Otherwise, the source subnet signs the message.
		signingSubnet = r.relayer.sourceSubnetID
	}

	height, err := r.relayer.pChainClient.GetHeight(context.Background())
	if err != nil {
		r.relayer.logger.Error(
			"Failed to get P-Chain height",
			zap.Error(err),
		)
		return nil, 0, err
	}

	// Get the current canonical validator set of the source subnet.
	canonicalSubnetValidators, totalValidatorWeight, err := avalancheWarp.GetCanonicalValidatorSet(
		context.Background(),
		r.relayer.canonicalValidatorClient,
		height,
		signingSubnet,
	)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to get the canonical subnet validator set",
			zap.String("subnetID", r.relayer.sourceSubnetID.String()),
			zap.Error(err),
		)
		return nil, 0, err
	}

	return canonicalSubnetValidators, totalValidatorWeight, nil
}

// isValidSignatureResponse tries to generate a signature from the peer.AsyncResponse, then verifies the signature against the node's public key.
// If we are unable to generate the signature or verify correctly, false will be returned to indicate no valid signature was found in response.
func (r *messageRelayer) isValidSignatureResponse(
	response message.InboundMessage,
	pubKey *bls.PublicKey,
) (blsSignatureBuf, bool) {
	// If the handler returned an error response, count the response and continue
	if response.Op() == message.AppErrorOp {
		r.relayer.logger.Debug(
			"Relayer async response failed",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	appResponse, ok := response.Message().(*p2p.AppResponse)
	if !ok {
		r.relayer.logger.Debug(
			"Relayer async response was not an AppResponse",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	var sigResponse msg.SignatureResponse
	if _, err := msg.Codec.Unmarshal(appResponse.AppBytes, &sigResponse); err != nil {
		r.relayer.logger.Error(
			"Error unmarshaling signature response",
			zap.Error(err),
		)
	}
	signature := sigResponse.Signature

	// If the node returned an empty signature, then it has not yet seen the warp message. Retry later.
	emptySignature := blsSignatureBuf{}
	if bytes.Equal(signature[:], emptySignature[:]) {
		r.relayer.logger.Debug(
			"Response contained an empty signature",
			zap.String("nodeID", response.NodeID().String()),
			zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	sig, err := bls.SignatureFromBytes(signature[:])
	if err != nil {
		r.relayer.logger.Debug(
			"Failed to create signature from response",
			zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	if !bls.Verify(pubKey, sig, r.warpMessage.Bytes()) {
		r.relayer.logger.Debug(
			"Failed verification for signature",
			zap.String("destinationBlockchainID", r.destinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	return signature, true
}

// aggregateSignatures constructs a BLS aggregate signature from the collected validator signatures. Also returns a bit set representing the
// validators that are represented in the aggregate signature. The bit set is in canonical validator order.
func (r *messageRelayer) aggregateSignatures(signatureMap map[int]blsSignatureBuf) (*bls.Signature, set.Bits, error) {
	// Aggregate the signatures
	signatures := make([]*bls.Signature, 0, len(signatureMap))
	vdrBitSet := set.NewBits()

	for i, sigBytes := range signatureMap {
		sig, err := bls.SignatureFromBytes(sigBytes[:])
		if err != nil {
			r.relayer.logger.Error(
				"Failed to unmarshal signature",
				zap.Error(err),
			)
			return nil, set.Bits{}, err
		}
		signatures = append(signatures, sig)
		vdrBitSet.Add(i)
	}

	aggSig, err := bls.AggregateSignatures(signatures)
	if err != nil {
		r.relayer.logger.Error(
			"Failed to aggregate signatures",
			zap.Error(err),
		)
		return nil, set.Bits{}, err
	}
	return aggSig, vdrBitSet, nil
}

func (r *messageRelayer) incSuccessfulRelayMessageCount() {
	r.relayer.metrics.successfulRelayMessageCount.
		WithLabelValues(
			r.destinationBlockchainID.String(),
			r.relayer.sourceBlockchainID.String(),
			r.relayer.sourceSubnetID.String()).Inc()
}

func (r *messageRelayer) incFailedRelayMessageCount(failureReason string) {
	r.relayer.metrics.failedRelayMessageCount.
		WithLabelValues(
			r.destinationBlockchainID.String(),
			r.relayer.sourceBlockchainID.String(),
			r.relayer.sourceSubnetID.String(),
			failureReason).Inc()
}

func (r *messageRelayer) setCreateSignedMessageLatencyMS(latency float64) {
	r.relayer.metrics.createSignedMessageLatencyMS.
		WithLabelValues(
			r.destinationBlockchainID.String(),
			r.relayer.sourceBlockchainID.String(),
			r.relayer.sourceSubnetID.String()).Set(latency)
}
