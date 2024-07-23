// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/lib/peers"
	"github.com/ava-labs/awm-relayer/utils"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	"go.uber.org/zap"
)

type blsSignatureBuf [bls.SignatureLen]byte

const (
	// Number of retries to collect signatures from validators
	maxRelayerQueryAttempts = 5
	// Maximum amount of time to spend waiting (in addition to network round trip time per attempt)
	// during relayer signature query routine
	signatureRequestRetryWaitPeriodMs = 10_000
)

var (
	codec        = msg.Codec
	coreEthCodec = coreEthMsg.Codec

	// Errors
	errNotEnoughSignatures     = errors.New("failed to collect a threshold of signatures")
	errFailedToGetAggSig       = errors.New("failed to get aggregate signature from node endpoint")
	errNotEnoughConnectedStake = errors.New("failed to connect to a threshold of stake")
)

type SignatureAggregator struct {
	network                 peers.AppRequestNetwork
	subnetIDsByBlockchainID map[ids.ID]ids.ID
	logger                  logging.Logger
	messageCreator          message.Creator
	currentRequestID        atomic.Uint32
	mu                      sync.RWMutex
}

func NewSignatureAggregator(
	network peers.AppRequestNetwork,
	logger logging.Logger,
	messageCreator message.Creator,
) *SignatureAggregator {
	sa := SignatureAggregator{
		network:                 network,
		subnetIDsByBlockchainID: map[ids.ID]ids.ID{},
		logger:                  logger,
		messageCreator:          messageCreator,
		currentRequestID:        atomic.Uint32{},
	}
	sa.currentRequestID.Store(rand.Uint32())
	return &sa
}

func (s *SignatureAggregator) AggregateSignaturesAppRequest(unsignedMessage *avalancheWarp.UnsignedMessage, inputSigningSubnet *ids.ID, quorumPercentage uint64) (*avalancheWarp.Message, error) {
	requestID := s.currentRequestID.Add(1)

	var signingSubnet ids.ID
	var err error
	// If signingSubnet is not set  we default to the subnet of the source blockchain
	sourceSubnet, err := s.GetSubnetID(unsignedMessage.SourceChainID)
	if err != nil {
		return nil, fmt.Errorf("Source message subnet not found for chainID %s", unsignedMessage.SourceChainID)
	}
	if inputSigningSubnet == nil {
		signingSubnet = sourceSubnet
	} else {
		signingSubnet = *inputSigningSubnet
	}

	connectedValidators, err := s.network.ConnectToCanonicalValidators(signingSubnet)

	if err != nil {
		s.logger.Error(
			"Failed to connect to canonical validators",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}
	if !utils.CheckStakeWeightPercentageExceedsThreshold(
		big.NewInt(0).SetUint64(connectedValidators.ConnectedWeight),
		connectedValidators.TotalValidatorWeight,
		quorumPercentage,
	) {
		s.logger.Error(
			"Failed to connect to a threshold of stake",
			zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
			zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
			zap.Uint64("quorumPercentage", quorumPercentage),
		)
		return nil, errNotEnoughConnectedStake
	}

	// TODO: remove this special handling and replace with ACP-118 interface once available
	var reqBytes []byte
	if sourceSubnet == constants.PrimaryNetworkID {
		req := coreEthMsg.MessageSignatureRequest{
			MessageID: unsignedMessage.ID(),
		}
		reqBytes, err = coreEthMsg.RequestToBytes(coreEthCodec, req)
	} else {
		req := msg.MessageSignatureRequest{
			MessageID: unsignedMessage.ID(),
		}
		reqBytes, err = msg.RequestToBytes(codec, req)
	}
	if err != nil {
		s.logger.Error(
			"Failed to marshal request bytes",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}

	// Construct the AppRequest
	outMsg, err := s.messageCreator.AppRequest(
		unsignedMessage.SourceChainID,
		requestID,
		peers.DefaultAppRequestTimeout,
		reqBytes,
	)
	if err != nil {
		s.logger.Error(
			"Failed to create app request message",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}

	// Query the validators with retries. On each retry, query one node per unique BLS pubkey
	accumulatedSignatureWeight := big.NewInt(0)

	signatureMap := make(map[int]blsSignatureBuf)
	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		responsesExpected := len(connectedValidators.ValidatorSet) - len(signatureMap)
		s.logger.Debug(
			"Aggregator collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
			zap.String("signingSubnetID", signingSubnet.String()),
			zap.Int("validatorSetSize", len(connectedValidators.ValidatorSet)),
			zap.Int("signatureMapSize", len(signatureMap)),
			zap.Int("responsesExpected", responsesExpected),
		)

		vdrSet := set.NewSet[ids.NodeID](len(connectedValidators.ValidatorSet))
		for i, vdr := range connectedValidators.ValidatorSet {
			// If we already have the signature for this validator, do not query any of the composite nodes again
			if _, ok := signatureMap[i]; ok {
				continue
			}

			// TODO: Track failures and iterate through the validator's node list on subsequent query attempts
			nodeID := vdr.NodeIDs[0]
			vdrSet.Add(nodeID)
			s.logger.Debug(
				"Added node ID to query.",
				zap.String("nodeID", nodeID.String()),
				zap.String("warpMessageID", unsignedMessage.ID().String()),
				zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
			)

			// Register a timeout response for each queried node
			reqID := ids.RequestID{
				NodeID:             nodeID,
				SourceChainID:      unsignedMessage.SourceChainID,
				DestinationChainID: unsignedMessage.SourceChainID,
				RequestID:          requestID,
				Op:                 byte(message.AppResponseOp),
			}
			s.network.RegisterAppRequest(reqID)
		}
		responseChan := s.network.RegisterRequestID(requestID, vdrSet.Len())

		sentTo := s.network.Send(outMsg, vdrSet, sourceSubnet, subnets.NoOpAllower)
		s.logger.Debug(
			"Sent signature request to network",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Any("sentTo", sentTo),
			zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
			zap.String("sourceSubnetID", sourceSubnet.String()),
			zap.String("signingSubnetID", signingSubnet.String()),
		)
		for nodeID := range vdrSet {
			if !sentTo.Contains(nodeID) {
				s.logger.Warn(
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
			for response := range responseChan {
				s.logger.Debug(
					"Processing response from node",
					zap.String("nodeID", response.NodeID().String()),
					zap.String("warpMessageID", unsignedMessage.ID().String()),
					zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
				)
				signedMsg, relevant, err := s.handleResponse(
					response,
					sentTo,
					requestID,
					connectedValidators,
					unsignedMessage,
					signatureMap,
					accumulatedSignatureWeight,
					quorumPercentage,
				)
				if err != nil {
					return nil, err
				}
				if relevant {
					responseCount++
				}
				// If we have sufficient signatures, return here.
				if signedMsg != nil {
					s.logger.Info(
						"Created signed message.",
						zap.String("warpMessageID", unsignedMessage.ID().String()),
						zap.Uint64("signatureWeight", accumulatedSignatureWeight.Uint64()),
						zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
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
	s.logger.Warn(
		"Failed to collect a threshold of signatures",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
		zap.Uint64("accumulatedWeight", accumulatedSignatureWeight.Uint64()),
		zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
	)
	return nil, errNotEnoughSignatures
}

func (s *SignatureAggregator) GetSubnetID(blockchainID ids.ID) (ids.ID, error) {
	s.mu.RLock()
	subnetID, ok := s.subnetIDsByBlockchainID[blockchainID]
	s.mu.RUnlock()
	if ok {
		return subnetID, nil
	}
	s.logger.Info("Signing subnet not found, requesting from PChain", zap.String("chainID", blockchainID.String()))
	subnetID, err := s.network.GetSubnetID(blockchainID)
	if err != nil {
		return ids.ID{}, fmt.Errorf("source blockchain not found for chain ID %s", blockchainID)
	}
	s.SetSubnetID(blockchainID, subnetID)
	return subnetID, nil
}

func (s *SignatureAggregator) SetSubnetID(blockchainID ids.ID, subnetID ids.ID) {
	s.mu.Lock()
	s.subnetIDsByBlockchainID[blockchainID] = subnetID
	s.mu.Unlock()
}

// Attempts to create a signed warp message from the accumulated responses.
// Returns a non-nil Warp message if [accumulatedSignatureWeight] exceeds the signature verification threshold.
// Returns false in the second return parameter if the app response is not relevant to the current signature
// aggregation request. Returns an error only if a non-recoverable error occurs, otherwise returns a nil error
// to continue processing responses.
func (s *SignatureAggregator) handleResponse(
	response message.InboundMessage,
	sentTo set.Set[ids.NodeID],
	requestID uint32,
	connectedValidators *peers.ConnectedCanonicalValidators,
	unsignedMessage *avalancheWarp.UnsignedMessage,
	signatureMap map[int]blsSignatureBuf,
	accumulatedSignatureWeight *big.Int,
	quorumPercentage uint64,
) (*avalancheWarp.Message, bool, error) {
	// Regardless of the response's relevance, call it's finished handler once this function returns
	defer response.OnFinishedHandling()

	// Check if this is an expected response.
	m := response.Message()
	rcvReqID, ok := message.GetRequestID(m)
	if !ok {
		// This should never occur, since inbound message validity is already checked by the inbound handler
		s.logger.Error("Could not get requestID from message")
		return nil, false, nil
	}
	nodeID := response.NodeID()
	if !sentTo.Contains(nodeID) || rcvReqID != requestID {
		s.logger.Debug("Skipping irrelevant app response")
		return nil, false, nil
	}

	// If we receive an AppRequestFailed, then the request timed out.
	// This is still a relevant response, since we are no longer expecting a response from that node.
	if response.Op() == message.AppErrorOp {
		s.logger.Debug("Request timed out")
		return nil, true, nil
	}

	validator, vdrIndex := connectedValidators.GetValidator(nodeID)
	signature, valid := s.isValidSignatureResponse(unsignedMessage, response, validator.PublicKey)
	if valid {
		s.logger.Debug(
			"Got valid signature response",
			zap.String("nodeID", nodeID.String()),
			zap.Uint64("stakeWeight", validator.Weight),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
		)
		signatureMap[vdrIndex] = signature
		accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))
	} else {
		s.logger.Debug(
			"Got invalid signature response",
			zap.String("nodeID", nodeID.String()),
			zap.Uint64("stakeWeight", validator.Weight),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
		)
		return nil, true, nil
	}

	// As soon as the signatures exceed the stake weight threshold we try to aggregate and send the transaction.
	if utils.CheckStakeWeightPercentageExceedsThreshold(
		accumulatedSignatureWeight,
		connectedValidators.TotalValidatorWeight,
		quorumPercentage,
	) {
		aggSig, vdrBitSet, err := s.aggregateSignatures(signatureMap)
		if err != nil {
			s.logger.Error(
				"Failed to aggregate signature.",
				zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
				zap.String("warpMessageID", unsignedMessage.ID().String()),
				zap.Error(err),
			)
			return nil, true, err
		}

		signedMsg, err := avalancheWarp.NewMessage(
			unsignedMessage,
			&avalancheWarp.BitSetSignature{
				Signers:   vdrBitSet.Bytes(),
				Signature: *(*[bls.SignatureLen]byte)(bls.SignatureToBytes(aggSig)),
			},
		)
		if err != nil {
			s.logger.Error(
				"Failed to create new signed message",
				zap.String("sourceBlockchainID", unsignedMessage.SourceChainID.String()),
				zap.String("warpMessageID", unsignedMessage.ID().String()),
				zap.Error(err),
			)
			return nil, true, err
		}
		return signedMsg, true, nil
	}
	// Not enough signatures, continue processing messages
	return nil, true, nil
}

// isValidSignatureResponse tries to generate a signature from the peer.AsyncResponse, then verifies
// the signature against the node's public key. If we are unable to generate the signature or verify
// correctly, false will be returned to indicate no valid signature was found in response.
func (s *SignatureAggregator) isValidSignatureResponse(
	unsignedMessage *avalancheWarp.UnsignedMessage,
	response message.InboundMessage,
	pubKey *bls.PublicKey,
) (blsSignatureBuf, bool) {
	// If the handler returned an error response, count the response and continue
	if response.Op() == message.AppErrorOp {
		s.logger.Debug(
			"Relayer async response failed",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	appResponse, ok := response.Message().(*p2p.AppResponse)
	if !ok {
		s.logger.Debug(
			"Relayer async response was not an AppResponse",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	var sigResponse msg.SignatureResponse
	if _, err := msg.Codec.Unmarshal(appResponse.AppBytes, &sigResponse); err != nil {
		s.logger.Error(
			"Error unmarshaling signature response",
			zap.Error(err),
		)
	}
	signature := sigResponse.Signature

	// If the node returned an empty signature, then it has not yet seen the warp message. Retry later.
	emptySignature := blsSignatureBuf{}
	if bytes.Equal(signature[:], emptySignature[:]) {
		s.logger.Debug(
			"Response contained an empty signature",
			zap.String("nodeID", response.NodeID().String()),
		)
		return blsSignatureBuf{}, false
	}

	sig, err := bls.SignatureFromBytes(signature[:])
	if err != nil {
		s.logger.Debug(
			"Failed to create signature from response",
		)
		return blsSignatureBuf{}, false
	}

	if !bls.Verify(pubKey, sig, unsignedMessage.Bytes()) {
		s.logger.Debug(
			"Failed verification for signature",
		)
		return blsSignatureBuf{}, false
	}

	return signature, true
}

// aggregateSignatures constructs a BLS aggregate signature from the collected validator signatures. Also
// returns a bit set representing the validators that are represented in the aggregate signature. The bit
// set is in canonical validator order.
func (s *SignatureAggregator) aggregateSignatures(
	signatureMap map[int]blsSignatureBuf,
) (*bls.Signature, set.Bits, error) {
	// Aggregate the signatures
	signatures := make([]*bls.Signature, 0, len(signatureMap))
	vdrBitSet := set.NewBits()

	for i, sigBytes := range signatureMap {
		sig, err := bls.SignatureFromBytes(sigBytes[:])
		if err != nil {
			s.logger.Error(
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
		s.logger.Error(
			"Failed to aggregate signatures",
			zap.Error(err),
		)
		return nil, set.Bits{}, err
	}
	return aggSig, vdrBitSet, nil
}
