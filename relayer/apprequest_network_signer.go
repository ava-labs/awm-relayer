package relayer

import (
	"bytes"
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
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/utils"
	"go.uber.org/zap"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
)

type AppRequestNetwork interface {
	ConnectToCanonicalValidators(subnetID ids.ID) (*peers.ConnectedCanonicalValidators, error)
	RegisterAppRequest(reqID ids.RequestID)
	RegisterRequestID(requestID uint32, numExpectedResponses int) chan message.InboundMessage
	Send(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], subnetID ids.ID, allower subnets.Allower) set.Set[ids.NodeID]
}

func NewAppRequestMessageSigner(
	logger logging.Logger,
	network AppRequestNetwork,
	srcBlockchainID ids.ID,
	srcBlockchainSubnetID ids.ID,
	destBlockchainID ids.ID,
	messageCreator message.Creator,
	signingSubnetID ids.ID,
	warpQuorum config.WarpQuorum,
	requestID uint32,
) *AppRequestMessageSigner {
	return &AppRequestMessageSigner{
		logger:                logger,
		network:               network,
		srcBlockchainID:       srcBlockchainID,
		srcBlockchainSubnetID: srcBlockchainSubnetID,
		destBlockchainID:      destBlockchainID,
		messageCreator:        messageCreator,
		signingSubnetID:       signingSubnetID,
		warpQuorum:            warpQuorum,
		requestID:             requestID,
	}
}

type AppRequestMessageSigner struct {
	logger                logging.Logger
	network               AppRequestNetwork
	srcBlockchainID       ids.ID
	srcBlockchainSubnetID ids.ID
	destBlockchainID      ids.ID
	messageCreator        message.Creator
	signingSubnetID       ids.ID
	warpQuorum            config.WarpQuorum
	requestID             uint32
}

func (s *AppRequestMessageSigner) SignMessage(unsignedMessage *avalancheWarp.UnsignedMessage) (*avalancheWarp.Message, error) {
	s.logger.Info("Fetching aggregate signature from the source chain validators via AppRequest")
	s.logger.Info(
		"Fetching aggregate signature from the source chain validators via AppRequest",
		zap.String("warpMessageID", unsignedMessage.ID().String()),
		zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
		zap.String("destinationBlockchainID", s.destBlockchainID.String()),
	)
	connectedValidators, err := s.network.ConnectToCanonicalValidators(s.signingSubnetID)
	if err != nil {
		s.logger.Error(
			"Failed to connect to canonical validators",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}
	if !utils.CheckStakeWeightExceedsThreshold(
		big.NewInt(0).SetUint64(connectedValidators.ConnectedWeight),
		connectedValidators.TotalValidatorWeight,
		s.warpQuorum.QuorumNumerator,
		s.warpQuorum.QuorumDenominator,
	) {
		s.logger.Error(
			"Failed to connect to a threshold of stake",
			zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
			zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
			zap.Any("warpQuorum", s.warpQuorum),
		)
		return nil, errNotEnoughConnectedStake
	}

	// Make sure to use the correct codec
	var reqBytes []byte
	if s.srcBlockchainSubnetID == constants.PrimaryNetworkID {
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
	outMsg, err := s.messageCreator.AppRequest(unsignedMessage.SourceChainID, s.requestID, peers.DefaultAppRequestTimeout, reqBytes)
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
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
			zap.String("destinationBlockchainID", s.destBlockchainID.String()),
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
				zap.String("destinationBlockchainID", s.destBlockchainID.String()),
				zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
			)

			// Register a timeout response for each queried node
			reqID := ids.RequestID{
				NodeID:             nodeID,
				SourceChainID:      unsignedMessage.SourceChainID,
				DestinationChainID: unsignedMessage.SourceChainID,
				RequestID:          s.requestID,
				Op:                 byte(message.AppResponseOp),
			}
			s.network.RegisterAppRequest(reqID)
		}
		responseChan := s.network.RegisterRequestID(s.requestID, vdrSet.Len())

		sentTo := s.network.Send(outMsg, vdrSet, s.srcBlockchainSubnetID, subnets.NoOpAllower)
		s.logger.Debug(
			"Sent signature request to network",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Any("sentTo", sentTo),
			zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
			zap.String("destinationBlockchainID", s.destBlockchainID.String()),
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
					zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
					zap.String("destinationBlockchainID", s.destBlockchainID.String()),
				)
				signedMsg, relevant, err := handleResponse(
					response,
					sentTo,
					s.requestID,
					connectedValidators,
					unsignedMessage,
					signatureMap,
					accumulatedSignatureWeight,
					s.warpQuorum,
					s.logger,
					s.srcBlockchainID,
					s.destBlockchainID,
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
						zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
						zap.String("destinationBlockchainID", s.destBlockchainID.String()),
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
		zap.String("sourceBlockchainID", s.srcBlockchainID.String()),
		zap.String("destinationBlockchainID", s.destBlockchainID.String()),
	)
	return nil, errNotEnoughSignatures
}

func handleResponse(
	response message.InboundMessage,
	sentTo set.Set[ids.NodeID],
	requestID uint32,
	connectedValidators *peers.ConnectedCanonicalValidators,
	unsignedMessage *avalancheWarp.UnsignedMessage,
	signatureMap map[int]blsSignatureBuf,
	accumulatedSignatureWeight *big.Int,
	warpQuorum config.WarpQuorum,
	logger logging.Logger,
	srcBlockchainID ids.ID,
	destBlockchainID ids.ID,
) (*avalancheWarp.Message, bool, error) {
	// Regardless of the response's relevance, call it's finished handler once this function returns
	defer response.OnFinishedHandling()

	// Check if this is an expected response.
	m := response.Message()
	rcvReqID, ok := message.GetRequestID(m)
	if !ok {
		// This should never occur, since inbound message validity is already checked by the inbound handler
		logger.Error("Could not get requestID from message")
		return nil, false, nil
	}
	nodeID := response.NodeID()
	if !sentTo.Contains(nodeID) || rcvReqID != requestID {
		logger.Debug("Skipping irrelevant app response")
		return nil, false, nil
	}

	// If we receive an AppRequestFailed, then the request timed out.
	// This is still a relevant response, since we are no longer expecting a response from that node.
	if response.Op() == message.AppErrorOp {
		logger.Debug("Request timed out")
		return nil, true, nil
	}

	validator, vdrIndex := connectedValidators.GetValidator(nodeID)
	signature, err := validateSignatureResponse(unsignedMessage, response, validator.PublicKey)
	if err != nil {
		logger.Debug(
			"Got invalid signature response",
			zap.String("nodeID", nodeID.String()),
			zap.Uint64("stakeWeight", validator.Weight),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("sourceBlockchainID", srcBlockchainID.String()),
			zap.String("destinationBlockchainID", destBlockchainID.String()),
			zap.Error(err),
		)
		return nil, true, nil
	}

	logger.Debug(
		"Got valid signature response",
		zap.String("nodeID", nodeID.String()),
		zap.Uint64("stakeWeight", validator.Weight),
		zap.String("warpMessageID", unsignedMessage.ID().String()),
		zap.String("sourceBlockchainID", srcBlockchainID.String()),
		zap.String("destinationBlockchainID", destBlockchainID.String()),
	)
	signatureMap[vdrIndex] = signature
	accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))

	// As soon as the signatures exceed the stake weight threshold we try to aggregate and send the transaction.
	if utils.CheckStakeWeightExceedsThreshold(
		accumulatedSignatureWeight,
		connectedValidators.TotalValidatorWeight,
		warpQuorum.QuorumNumerator,
		warpQuorum.QuorumDenominator,
	) {
		aggSig, vdrBitSet, err := aggregateSignatures(signatureMap)
		if err != nil {
			logger.Error(
				"Failed to aggregate signature.",
				zap.String("sourceBlockchainID", srcBlockchainID.String()),
				zap.String("destinationBlockchainID", destBlockchainID.String()),
				zap.String("warpMessageID", unsignedMessage.ID().String()),
				zap.Error(err),
			)
			return nil, true, err
		}

		signedMsg, err := avalancheWarp.NewMessage(unsignedMessage, &avalancheWarp.BitSetSignature{
			Signers:   vdrBitSet.Bytes(),
			Signature: *(*[bls.SignatureLen]byte)(bls.SignatureToBytes(aggSig)),
		})
		if err != nil {
			logger.Error(
				"Failed to create new signed message",
				zap.String("sourceBlockchainID", srcBlockchainID.String()),
				zap.String("destinationBlockchainID", srcBlockchainID.String()),
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

// aggregateSignatures constructs a BLS aggregate signature from the collected validator signatures. Also returns a bit set representing the
// validators that are represented in the aggregate signature. The bit set is in canonical validator order.
func aggregateSignatures(signatureMap map[int]blsSignatureBuf) (*bls.Signature, set.Bits, error) {
	// Aggregate the signatures
	signatures := make([]*bls.Signature, 0, len(signatureMap))
	vdrBitSet := set.NewBits()

	for i, sigBytes := range signatureMap {
		sig, err := bls.SignatureFromBytes(sigBytes[:])
		if err != nil {
			return nil, set.Bits{}, fmt.Errorf("failed to unmarshal signature: %v", err)
		}
		signatures = append(signatures, sig)
		vdrBitSet.Add(i)
	}

	aggSig, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return nil, set.Bits{}, fmt.Errorf("failed to aggregate signatures: %v", err)
	}
	return aggSig, vdrBitSet, nil
}

// isValidSignatureResponse tries to generate a signature from the peer.AsyncResponse, then verifies the signature against the node's public key.
// If we are unable to generate the signature or verify correctly, false will be returned to indicate no valid signature was found in response.
func validateSignatureResponse(
	unsignedMessage *avalancheWarp.UnsignedMessage,
	response message.InboundMessage,
	pubKey *bls.PublicKey,
) (blsSignatureBuf, error) {
	// If the handler returned an error response, count the response and continue
	if response.Op() == message.AppErrorOp {
		return blsSignatureBuf{}, fmt.Errorf("relayer async response failed")
	}

	appResponse, ok := response.Message().(*p2p.AppResponse)
	if !ok {
		return blsSignatureBuf{}, fmt.Errorf("relayer async response was not an AppResponse")
	}

	var sigResponse msg.SignatureResponse
	if _, err := msg.Codec.Unmarshal(appResponse.AppBytes, &sigResponse); err != nil {
		return blsSignatureBuf{}, fmt.Errorf("error unmarshaling signature response: %v", err)
	}
	signature := sigResponse.Signature

	// If the node returned an empty signature, then it has not yet seen the warp message. Retry later.
	emptySignature := blsSignatureBuf{}
	if bytes.Equal(signature[:], emptySignature[:]) {
		return blsSignatureBuf{}, fmt.Errorf("response contained an empty signature")
	}

	sig, err := bls.SignatureFromBytes(signature[:])
	if err != nil {
		return blsSignatureBuf{}, fmt.Errorf("failed to create signature from response: %v", err)
	}

	if !bls.Verify(pubKey, sig, unsignedMessage.Bytes()) {
		return blsSignatureBuf{}, fmt.Errorf("failed verification for signature: %v", err)
	}

	return signature, nil
}
