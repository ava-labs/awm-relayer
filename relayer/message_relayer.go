// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/temp"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms/vmtypes"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
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
	codec = msg.Codec
	// Errors
	errNotEnoughSignatures = fmt.Errorf("failed to collect a threshold of signatures")
)

// messageRelayers are created for each warp message to be relayed.
// They collect signatures from validators, aggregate them,
// and send the signed warp message to the destination chain.
// Each messageRelayer runs in its own goroutine.
type messageRelayer struct {
	relayer             *Relayer
	warpMessage         *warp.UnsignedMessage
	messageResponseChan chan message.InboundMessage
	logger              logging.Logger
	metrics             *MessageRelayerMetrics
	messageCreator      message.Creator
}

func newMessageRelayer(
	logger logging.Logger,
	metrics *MessageRelayerMetrics,
	relayer *Relayer,
	warpMessage *warp.UnsignedMessage,
	messageResponseChan chan message.InboundMessage,
	messageCreator message.Creator,
) *messageRelayer {
	return &messageRelayer{
		relayer:             relayer,
		warpMessage:         warpMessage,
		messageResponseChan: messageResponseChan,
		logger:              logger,
		metrics:             metrics,
		messageCreator:      messageCreator,
	}
}

func (r *messageRelayer) run(warpMessageInfo *vmtypes.WarpMessageInfo, requestID uint32, messageManager messages.MessageManager) {
	shouldSend, err := messageManager.ShouldSendMessage(warpMessageInfo)
	if err != nil {
		r.logger.Error(
			"Failed to check if message should be sent",
			zap.Error(err),
		)

		r.incFailedRelayMessageCount("failed to check if message should be sent")

		r.relayer.errorChan <- err
		return
	}
	if !shouldSend {
		r.logger.Info("Message should not be sent")
		return
	}

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	signedMessage, err := r.createSignedMessage(requestID)
	if err != nil {
		r.logger.Error(
			"Failed to create signed warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to create signed warp message")
		r.relayer.errorChan <- err
		return
	}

	// create signed message latency (ms)
	r.setCreateSignedMessageLatencyMS(float64(time.Since(startCreateSignedMessageTime).Milliseconds()))

	err = messageManager.SendMessage(signedMessage, warpMessageInfo.WarpPayload)
	if err != nil {
		r.logger.Error(
			"Failed to send warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to send warp message")
		r.relayer.errorChan <- err
		return
	}
	r.logger.Info(
		"Finished relaying message to destination chain",
		zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
	)
	r.incSuccessfulRelayMessageCount()
}

// Run collects signatures from nodes by directly querying them via AppRequest, then aggregates the signatures, and constructs the signed warp message.
func (r *messageRelayer) createSignedMessage(requestID uint32) (*warp.Message, error) {
	r.logger.Info(
		"Starting relayer routine",
		zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
	)

	// Get the current canonical validator set of the source subnet.
	validatorSet, totalValidatorWeight, err := r.getCurrentCanonicalValidatorSet()
	if err != nil {
		r.logger.Error(
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
	for node, _ := range nodeValidatorIndexMap {
		nodeIDs.Add(node)
	}

	// TODO: We may still be able to proceed with signature aggregation even if we fail to connect to some peers.
	// 		 We should check if the connected set represents sufficient stake, and continue if so.
	_, err = r.relayer.network.ConnectPeers(nodeIDs)
	if err != nil {
		r.logger.Error(
			"Failed to connect to peers",
			zap.Error(err),
		)
		return nil, err
	}

	// Construct the request
	req := msg.SignatureRequest{
		MessageID: r.warpMessage.ID(),
	}
	reqBytes, err := msg.RequestToBytes(codec, req)
	if err != nil {
		r.logger.Error(
			"Failed to marshal request bytes",
			zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	outMsg, err := r.messageCreator.AppRequest(r.warpMessage.SourceChainID, requestID, peers.DefaultAppRequestTimeout, reqBytes)
	if err != nil {
		r.logger.Error(
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
		r.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
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
			r.logger.Debug(
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
		r.logger.Debug(
			"Sent signature request to network",
			zap.String("messageID", req.MessageID.String()),
			zap.Any("sentTo", sentTo),
		)
		for nodeID := range vdrSet {
			if !sentTo.Contains(nodeID) {
				r.logger.Warn(
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
			for response := range r.messageResponseChan {
				r.logger.Debug(
					"Processing response from node",
					zap.String("nodeID", response.NodeID().String()),
				)
				// This anonymous function attempts to create a signed warp message from the accumulated responses
				// Returns an error only if a non-recoverable error occurs, otherwise returns (nil, nil) to continue processing responses
				// When a non-nil signedMsg is returned, createSignedMessage itself returns
				signedMsg, err := func() (*warp.Message, error) {
					defer response.OnFinishedHandling()

					// Check if this is an expected response.
					m := response.Message()
					rcvReqID, ok := message.GetRequestID(m)
					if !ok {
						// This should never occur, since inbound message validity is already checked by the inbound handler
						r.logger.Error("Could not get requestID from message")
						return nil, nil
					}
					nodeID := response.NodeID()
					if !sentTo.Contains(nodeID) || rcvReqID != requestID {
						r.logger.Debug("Skipping irrelevant app response")
						return nil, nil
					}

					// Count the relevant app message
					responseCount++

					// If we receive an AppRequestFailed, then the request timed out.
					// We still want to increment responseCount, since we are no longer expecting a response from that node.
					if response.Op() == message.AppRequestFailedOp {
						r.logger.Debug("Request timed out")
						return nil, nil
					}

					validator := validatorSet[nodeValidatorIndexMap[nodeID]]
					signature, valid := r.isValidSignatureResponse(response, validator.PublicKey)
					if valid {
						r.logger.Debug(
							"Got valid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						signatureMap[nodeValidatorIndexMap[nodeID]] = signature
						accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))
					} else {
						r.logger.Debug(
							"Got invalid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						return nil, nil
					}

					// As soon as the signatures exceed the stake weight threshold we try to aggregate and send the transaction.
					if utils.CheckStakeWeightExceedsThreshold(accumulatedSignatureWeight, totalValidatorWeight) {
						aggSig, vdrBitSet, err := r.aggregateSignatures(signatureMap)
						if err != nil {
							r.logger.Error(
								"Failed to aggregate signature.",
								zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
								zap.Error(err),
							)
							return nil, err
						}

						signedMsg, err := warp.NewMessage(r.warpMessage, &warp.BitSetSignature{
							Signers:   vdrBitSet.Bytes(),
							Signature: *(*[bls.SignatureLen]byte)(bls.SignatureToBytes(aggSig)),
						})
						if err != nil {
							r.logger.Error(
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
					r.logger.Info(
						"Created signed message.",
						zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
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

	r.logger.Warn(
		"Failed to collect a threshold of signatures",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
	)
	return nil, errNotEnoughSignatures
}

func (r *messageRelayer) getCurrentCanonicalValidatorSet() ([]*warp.Validator, uint64, error) {
	// GetCurrentValidators currently does not return BLS public key and proof of possession for subnets.
	// That information is only included for primary network validators, so we first look up the current primary
	// network validators and index them on their node ID. Then we lookup the subnet validators, and match
	// their BLS public key and proof of possession with that of the primary network validator with the same node ID.
	// TODO: Check with platform team if the "signer" information could be returned for the specific subnets' validators.
	primaryNetworkValidators, err := r.relayer.pChainClient.GetCurrentValidators(context.Background(), ids.Empty, nil)
	if err != nil {
		r.logger.Error(
			"Failed to get current primary network validator set",
			zap.Error(err),
		)
		return nil, 0, err
	}
	primaryNetworkSigners := make(map[ids.NodeID]*signer.ProofOfPossession)
	for _, validator := range primaryNetworkValidators {
		r.logger.Debug(
			"Set primary network BLS signer for node",
			zap.String("nodeID", validator.NodeID.String()),
		)
		primaryNetworkSigners[validator.NodeID] = validator.Signer
	}

	var signingSubnet ids.ID
	if r.relayer.sourceSubnetID == constants.PrimaryNetworkID {
		// If the message originates from the primary subnet, then we instead "self sign" the message using the validators of the destination subnet.
		signingSubnet, err = r.relayer.pChainClient.ValidatedBy(context.Background(), r.warpMessage.DestinationChainID)
		if err != nil {
			r.logger.Error(
				"failed to get validating subnet for destination chain",
				zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
				zap.Error(err),
			)
			return nil, 0, err
		}
	} else {
		// Otherwise, the source subnet signs the message.
		signingSubnet = r.relayer.sourceSubnetID
	}
	subnetValidators, err := r.relayer.pChainClient.GetCurrentValidators(context.Background(), signingSubnet, nil)
	if err != nil {
		r.logger.Error(
			"Failed to get the current validator set",
			zap.String("subnetID", r.relayer.sourceSubnetID.String()),
			zap.Error(err),
		)
		return nil, 0, err
	}

	for i := 0; i < len(subnetValidators); i++ {
		if primaryNetworkSigner, ok := primaryNetworkSigners[subnetValidators[i].NodeID]; ok {
			subnetValidators[i].Signer = primaryNetworkSigner
			r.logger.Debug(
				"Set subnet BLS signer for node",
				zap.String("nodeID", subnetValidators[i].NodeID.String()),
			)
		} else {
			r.logger.Debug(
				"Missing BLS signer info for node",
				zap.String("nodeID", subnetValidators[i].NodeID.String()),
			)
		}
	}
	// Get the canonical validator set. The returned slice consists of virtual validators, each represented by a single BLS pubkey and composed of multiple individual nodes
	// This is because BLS keys are not unique, and may be shared by multiple nodeIDs, which are the entities we need to query
	canonicalSubnetValidators, totalValidatorWeight, err := temp.GetCanonicalValidatorSet(subnetValidators)
	if err != nil {
		r.logger.Error(
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
	pubKey *bls.PublicKey) (blsSignatureBuf, bool) {

	// If the handler returned an error response, count the response and continue
	if response.Op() == message.AppRequestFailedOp {
		r.logger.Debug(
			"Relayer async response failed",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	appResponse, ok := response.Message().(*p2p.AppResponse)
	if !ok {
		r.logger.Debug(
			"Relayer async response was not an AppResponse",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	var sigResponse msg.SignatureResponse
	if _, err := msg.Codec.Unmarshal(appResponse.AppBytes, &sigResponse); err != nil {
		r.logger.Error(
			"Error unmarshaling signature response",
			zap.Error(err),
		)
	}
	signature := sigResponse.Signature

	// If the node returned an empty signature, then it has not yet seen the warp message. Retry later.
	emptySignature := blsSignatureBuf{}
	if bytes.Equal(signature[:], emptySignature[:]) {
		r.logger.Debug(
			"Response contained an empty signature",
			zap.String("nodeID", response.NodeID().String()),
			zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	sig, err := bls.SignatureFromBytes(signature[:])
	if err != nil {
		r.logger.Debug(
			"Failed to create signature from response",
			zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	if !bls.Verify(pubKey, sig, r.warpMessage.Bytes()) {
		r.logger.Debug(
			"Failed verification for signature",
			zap.String("destinationChainID", r.warpMessage.DestinationChainID.String()),
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
			r.logger.Error(
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
		r.logger.Error(
			"Failed to aggregate signatures",
			zap.Error(err),
		)
		return nil, set.Bits{}, err
	}
	return aggSig, vdrBitSet, nil
}

func (r *messageRelayer) incSuccessfulRelayMessageCount() {
	r.metrics.successfulRelayMessageCount.
		WithLabelValues(
			r.warpMessage.DestinationChainID.String(),
			r.relayer.sourceChainID.String(),
			r.relayer.sourceSubnetID.String()).Inc()
}

func (r *messageRelayer) incFailedRelayMessageCount(failureReason string) {
	r.metrics.failedRelayMessageCount.
		WithLabelValues(
			r.warpMessage.DestinationChainID.String(),
			r.relayer.sourceChainID.String(),
			r.relayer.sourceSubnetID.String(),
			failureReason).Inc()
}

func (r *messageRelayer) setCreateSignedMessageLatencyMS(latency float64) {
	r.metrics.createSignedMessageLatencyMS.
		WithLabelValues(
			r.warpMessage.DestinationChainID.String(),
			r.relayer.sourceChainID.String(),
			r.relayer.sourceSubnetID.String()).Set(latency)
}
