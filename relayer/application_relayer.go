// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"math/rand"
	"sync"
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
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/awm-relayer/relayer/checkpoint"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/awm-relayer/vms"
	coreEthMsg "github.com/ava-labs/coreth/plugin/evm/message"
	msg "github.com/ava-labs/subnet-evm/plugin/evm/message"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
	"golang.org/x/sync/errgroup"

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
	errNotEnoughSignatures     = errors.New("failed to collect a threshold of signatures")
	errFailedToGetAggSig       = errors.New("failed to get aggregate signature from node endpoint")
	errNotEnoughConnectedStake = errors.New("failed to connect to a threshold of stake")
)

// ApplicationRelayers define a Warp message route from a specific source address on a specific source blockchain
// to a specific destination address on a specific destination blockchain. This routing information is
// encapsulated in [relayerID], which also represents the database key for an ApplicationRelayer.
type ApplicationRelayer struct {
	logger            logging.Logger
	metrics           *ApplicationRelayerMetrics
	network           *peers.AppRequestNetwork
	messageCreator    message.Creator
	sourceBlockchain  config.SourceBlockchain
	signingSubnetID   ids.ID
	destinationClient vms.DestinationClient
	relayerID         database.RelayerID
	warpQuorum        config.WarpQuorum
	checkpointManager *checkpoint.CheckpointManager
	currentRequestID  uint32
	lock              *sync.RWMutex
}

func NewApplicationRelayer(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	network *peers.AppRequestNetwork,
	messageCreator message.Creator,
	relayerID database.RelayerID,
	db database.RelayerDatabase,
	ticker *utils.Ticker,
	destinationClient vms.DestinationClient,
	sourceBlockchain config.SourceBlockchain,
	startingHeight uint64,
	cfg *config.Config,
) (*ApplicationRelayer, error) {
	quorum, err := cfg.GetWarpQuorum(relayerID.DestinationBlockchainID)
	if err != nil {
		logger.Error(
			"Failed to get warp quorum from config. Relayer may not be configured to deliver to the destination chain.",
			zap.String("destinationBlockchainID", relayerID.DestinationBlockchainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	var signingSubnet ids.ID
	if sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
		// If the message originates from the primary subnet, then we instead "self sign" the message using the validators of the destination subnet.
		signingSubnet = cfg.GetSubnetID(relayerID.DestinationBlockchainID)
	} else {
		// Otherwise, the source subnet signs the message.
		signingSubnet = sourceBlockchain.GetSubnetID()
	}

	sub := ticker.Subscribe()

	checkpointManager := checkpoint.NewCheckpointManager(logger, db, sub, relayerID, startingHeight)
	checkpointManager.Run()

	ar := ApplicationRelayer{
		logger:            logger,
		metrics:           metrics,
		network:           network,
		messageCreator:    messageCreator,
		sourceBlockchain:  sourceBlockchain,
		destinationClient: destinationClient,
		relayerID:         relayerID,
		signingSubnetID:   signingSubnet,
		warpQuorum:        quorum,
		checkpointManager: checkpointManager,
		currentRequestID:  rand.Uint32(), // TODONOW: pass via ctor
		lock:              &sync.RWMutex{},
	}

	return &ar, nil
}

// Process [msgs] at height [height] by relaying each message to the destination chain.
// Checkpoints the height with the checkpoint manager when all messages are relayed.
// ProcessHeight is expected to be called for every block greater than or equal to the [startingHeight] provided in the constructor
func (r *ApplicationRelayer) ProcessHeight(height uint64, handlers []messages.MessageHandler, errChan chan error) {
	var eg errgroup.Group
	for _, handler := range handlers {
		// Copy the loop variable to a local variable to avoid the loop variable being captured by the goroutine
		// Once we upgrade to Go 1.22, we can use the loop variable directly in the goroutine
		h := handler
		eg.Go(func() error {
			return r.ProcessMessage(h)
		})
	}
	if err := eg.Wait(); err != nil {
		r.logger.Error(
			"Failed to process block",
			zap.Uint64("height", height),
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Error(err),
		)
		errChan <- err
		return
	}
	r.checkpointManager.StageCommittedHeight(height)
	r.logger.Debug(
		"Processed block",
		zap.Uint64("height", height),
		zap.String("sourceBlockchainID", r.relayerID.SourceBlockchainID.String()),
		zap.String("relayerID", r.relayerID.ID.String()),
		zap.Int("numMessages", len(handlers)),
	)
}

// Relays a message to the destination chain. Does not checkpoint the height.
func (r *ApplicationRelayer) ProcessMessage(handler messages.MessageHandler) error {
	// Increment the request ID. Make sure we don't hold the lock while we relay the message.
	r.lock.Lock()
	r.currentRequestID++
	reqID := r.currentRequestID
	r.lock.Unlock()

	err := r.relayMessage(
		reqID,
		handler,
	)

	return err
}

func (r *ApplicationRelayer) RelayerID() database.RelayerID {
	return r.relayerID
}

func (r *ApplicationRelayer) relayMessage(
	requestID uint32,
	handler messages.MessageHandler,
) error {
	r.logger.Debug(
		"Relaying message",
		zap.Uint32("requestID", requestID),
		zap.String("sourceBlockchainID", r.sourceBlockchain.BlockchainID),
		zap.String("relayerID", r.relayerID.ID.String()),
	)
	shouldSend, err := handler.ShouldSendMessage(r.destinationClient)
	if err != nil {
		r.logger.Error(
			"Failed to check if message should be sent",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to check if message should be sent")
		return err
	}
	if !shouldSend {
		r.logger.Info("Message should not be sent")
		return nil
	}
	unsignedMessage := handler.GetUnsignedMessage()

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	var signedMessage *avalancheWarp.Message
	if r.sourceBlockchain.UseAppRequestNetwork() {
		signedMessage, err = r.createSignedMessageAppRequest(unsignedMessage, requestID)
		if err != nil {
			r.logger.Error(
				"Failed to create signed warp message via AppRequest network",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via AppRequest network")
			return err
		}
	} else {
		signedMessage, err = r.createSignedMessage(unsignedMessage)
		if err != nil {
			r.logger.Error(
				"Failed to create signed warp message via RPC",
				zap.Error(err),
			)
			r.incFailedRelayMessageCount("failed to create signed warp message via RPC")
			return err
		}
	}

	// create signed message latency (ms)
	r.setCreateSignedMessageLatencyMS(float64(time.Since(startCreateSignedMessageTime).Milliseconds()))

	err = handler.SendMessage(signedMessage, r.destinationClient)
	if err != nil {
		r.logger.Error(
			"Failed to send warp message",
			zap.Error(err),
		)
		r.incFailedRelayMessageCount("failed to send warp message")
		return err
	}
	r.logger.Info(
		"Finished relaying message to destination chain",
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
	)
	r.incSuccessfulRelayMessageCount()

	return nil
}

// createSignedMessage fetches the signed Warp message from the source chain via RPC.
// Each VM may implement their own RPC method to construct the aggregate signature, which
// will need to be accounted for here.
func (r *ApplicationRelayer) createSignedMessage(unsignedMessage *avalancheWarp.UnsignedMessage) (*avalancheWarp.Message, error) {
	r.logger.Info("Fetching aggregate signature from the source chain validators via API")
	warpClient, err := warpBackend.NewClient(r.sourceBlockchain.WarpAPIEndpoint.BaseURL, r.sourceBlockchain.GetBlockchainID().String())
	if err != nil {
		r.logger.Error(
			"Failed to create Warp API client",
			zap.Error(err),
		)
		return nil, err
	}

	var signedWarpMessageBytes []byte
	for attempt := 1; attempt <= maxRelayerQueryAttempts; attempt++ {
		r.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
			zap.String("signingSubnetID", r.signingSubnetID.String()),
		)
		signedWarpMessageBytes, err = warpClient.GetMessageAggregateSignature(
			context.Background(),
			unsignedMessage.ID(),
			r.warpQuorum.QuorumNumerator,
			r.signingSubnetID.String(),
		)
		if err == nil {
			warpMsg, err := avalancheWarp.ParseMessage(signedWarpMessageBytes)
			if err != nil {
				r.logger.Error(
					"Failed to parse signed warp message",
					zap.Error(err),
				)
				return nil, err
			}
			return warpMsg, err
		}
		r.logger.Info(
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
	r.logger.Warn(
		"Failed to get aggregate signature from node endpoint",
		zap.Int("attempts", maxRelayerQueryAttempts),
		zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		zap.String("signingSubnetID", r.signingSubnetID.String()),
	)
	return nil, errFailedToGetAggSig
}

// createSignedMessageAppRequest collects signatures from nodes by directly querying them via AppRequest, then aggregates the signatures, and constructs the signed warp message.
func (r *ApplicationRelayer) createSignedMessageAppRequest(unsignedMessage *avalancheWarp.UnsignedMessage, requestID uint32) (*avalancheWarp.Message, error) {
	r.logger.Info(
		"Fetching aggregate signature from the source chain validators via AppRequest",
		zap.String("warpMessageID", unsignedMessage.ID().String()),
		zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
	)
	connectedValidators, err := r.network.ConnectToCanonicalValidators(r.signingSubnetID)
	if err != nil {
		r.logger.Error(
			"Failed to connect to canonical validators",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}
	if !utils.CheckStakeWeightExceedsThreshold(
		big.NewInt(0).SetUint64(connectedValidators.ConnectedWeight),
		connectedValidators.TotalValidatorWeight,
		r.warpQuorum.QuorumNumerator,
		r.warpQuorum.QuorumDenominator,
	) {
		r.logger.Error(
			"Failed to connect to a threshold of stake",
			zap.Uint64("connectedWeight", connectedValidators.ConnectedWeight),
			zap.Uint64("totalValidatorWeight", connectedValidators.TotalValidatorWeight),
			zap.Any("warpQuorum", r.warpQuorum),
		)
		return nil, errNotEnoughConnectedStake
	}

	// Make sure to use the correct codec
	var reqBytes []byte
	if r.sourceBlockchain.GetSubnetID() == constants.PrimaryNetworkID {
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
		r.logger.Error(
			"Failed to marshal request bytes",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Error(err),
		)
		return nil, err
	}

	// Construct the AppRequest
	outMsg, err := r.messageCreator.AppRequest(unsignedMessage.SourceChainID, requestID, peers.DefaultAppRequestTimeout, reqBytes)
	if err != nil {
		r.logger.Error(
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
		r.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
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
			r.logger.Debug(
				"Added node ID to query.",
				zap.String("nodeID", nodeID.String()),
				zap.String("warpMessageID", unsignedMessage.ID().String()),
				zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
				zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			)

			// Register a timeout response for each queried node
			reqID := ids.RequestID{
				NodeID:             nodeID,
				SourceChainID:      unsignedMessage.SourceChainID,
				DestinationChainID: unsignedMessage.SourceChainID,
				RequestID:          requestID,
				Op:                 byte(message.AppResponseOp),
			}
			r.network.Handler.RegisterAppRequest(reqID)
		}
		responseChan := r.network.Handler.RegisterRequestID(requestID, vdrSet.Len())

		sentTo := r.network.Network.Send(outMsg, vdrSet, r.sourceBlockchain.GetSubnetID(), subnets.NoOpAllower)
		r.logger.Debug(
			"Sent signature request to network",
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.Any("sentTo", sentTo),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
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
			for response := range responseChan {
				r.logger.Debug(
					"Processing response from node",
					zap.String("nodeID", response.NodeID().String()),
					zap.String("warpMessageID", unsignedMessage.ID().String()),
					zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
					zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
				)
				signedMsg, relevant, err := r.handleResponse(
					response,
					sentTo,
					requestID,
					connectedValidators,
					unsignedMessage,
					signatureMap,
					accumulatedSignatureWeight,
				)
				if err != nil {
					return nil, err
				}
				if relevant {
					responseCount++
				}
				// If we have sufficient signatures, return here.
				if signedMsg != nil {
					r.logger.Info(
						"Created signed message.",
						zap.String("warpMessageID", unsignedMessage.ID().String()),
						zap.Uint64("signatureWeight", accumulatedSignatureWeight.Uint64()),
						zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
						zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
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
		zap.String("warpMessageID", unsignedMessage.ID().String()),
		zap.Uint64("accumulatedWeight", accumulatedSignatureWeight.Uint64()),
		zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
	)
	return nil, errNotEnoughSignatures
}

// Attempts to create a signed warp message from the accumulated responses.
// Returns a non-nil Warp message if [accumulatedSignatureWeight] exceeds the signature verification threshold.
// Returns false in the second return parameter if the app response is not relevant to the current signature aggregation request.
// Returns an error only if a non-recoverable error occurs, otherwise returns a nil error to continue processing responses.
func (r *ApplicationRelayer) handleResponse(
	response message.InboundMessage,
	sentTo set.Set[ids.NodeID],
	requestID uint32,
	connectedValidators *peers.ConnectedCanonicalValidators,
	unsignedMessage *avalancheWarp.UnsignedMessage,
	signatureMap map[int]blsSignatureBuf,
	accumulatedSignatureWeight *big.Int,
) (*avalancheWarp.Message, bool, error) {
	// Regardless of the response's relevance, call it's finished handler once this function returns
	defer response.OnFinishedHandling()

	// Check if this is an expected response.
	m := response.Message()
	rcvReqID, ok := message.GetRequestID(m)
	if !ok {
		// This should never occur, since inbound message validity is already checked by the inbound handler
		r.logger.Error("Could not get requestID from message")
		return nil, false, nil
	}
	nodeID := response.NodeID()
	if !sentTo.Contains(nodeID) || rcvReqID != requestID {
		r.logger.Debug("Skipping irrelevant app response")
		return nil, false, nil
	}

	// If we receive an AppRequestFailed, then the request timed out.
	// This is still a relevant response, since we are no longer expecting a response from that node.
	if response.Op() == message.AppErrorOp {
		r.logger.Debug("Request timed out")
		return nil, true, nil
	}

	validator, vdrIndex := connectedValidators.GetValidator(nodeID)
	signature, valid := r.isValidSignatureResponse(unsignedMessage, response, validator.PublicKey)
	if valid {
		r.logger.Debug(
			"Got valid signature response",
			zap.String("nodeID", nodeID.String()),
			zap.Uint64("stakeWeight", validator.Weight),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		)
		signatureMap[vdrIndex] = signature
		accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))
	} else {
		r.logger.Debug(
			"Got invalid signature response",
			zap.String("nodeID", nodeID.String()),
			zap.Uint64("stakeWeight", validator.Weight),
			zap.String("warpMessageID", unsignedMessage.ID().String()),
			zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		)
		return nil, true, nil
	}

	// As soon as the signatures exceed the stake weight threshold we try to aggregate and send the transaction.
	if utils.CheckStakeWeightExceedsThreshold(
		accumulatedSignatureWeight,
		connectedValidators.TotalValidatorWeight,
		r.warpQuorum.QuorumNumerator,
		r.warpQuorum.QuorumDenominator,
	) {
		aggSig, vdrBitSet, err := r.aggregateSignatures(signatureMap)
		if err != nil {
			r.logger.Error(
				"Failed to aggregate signature.",
				zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
				zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
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
			r.logger.Error(
				"Failed to create new signed message",
				zap.String("sourceBlockchainID", r.sourceBlockchain.GetBlockchainID().String()),
				zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
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

// isValidSignatureResponse tries to generate a signature from the peer.AsyncResponse, then verifies the signature against the node's public key.
// If we are unable to generate the signature or verify correctly, false will be returned to indicate no valid signature was found in response.
func (r *ApplicationRelayer) isValidSignatureResponse(
	unsignedMessage *avalancheWarp.UnsignedMessage,
	response message.InboundMessage,
	pubKey *bls.PublicKey,
) (blsSignatureBuf, bool) {
	// If the handler returned an error response, count the response and continue
	if response.Op() == message.AppErrorOp {
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
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	sig, err := bls.SignatureFromBytes(signature[:])
	if err != nil {
		r.logger.Debug(
			"Failed to create signature from response",
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	if !bls.Verify(pubKey, sig, unsignedMessage.Bytes()) {
		r.logger.Debug(
			"Failed verification for signature",
			zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
		)
		return blsSignatureBuf{}, false
	}

	return signature, true
}

// aggregateSignatures constructs a BLS aggregate signature from the collected validator signatures. Also returns a bit set representing the
// validators that are represented in the aggregate signature. The bit set is in canonical validator order.
func (r *ApplicationRelayer) aggregateSignatures(signatureMap map[int]blsSignatureBuf) (*bls.Signature, set.Bits, error) {
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

//
// Metrics
//

func (r *ApplicationRelayer) incSuccessfulRelayMessageCount() {
	r.metrics.successfulRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Inc()
}

func (r *ApplicationRelayer) incFailedRelayMessageCount(failureReason string) {
	r.metrics.failedRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String(),
			failureReason).Inc()
}

func (r *ApplicationRelayer) setCreateSignedMessageLatencyMS(latency float64) {
	r.metrics.createSignedMessageLatencyMS.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Set(latency)
}
