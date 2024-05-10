// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
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
	"github.com/ava-labs/awm-relayer/ethclient"
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
	errNotEnoughSignatures     = errors.New("failed to collect a threshold of signatures")
	errFailedToGetAggSig       = errors.New("failed to get aggregate signature from node endpoint")
	errNotEnoughConnectedStake = errors.New("failed to connect to a threshold of stake")
)

// applicationRelayers are created for each warp message to be relayed.
// They collect signatures from validators, aggregate them,
// and send the signed warp message to the destination chain.
// Each applicationRelayer runs in its own goroutine.
type applicationRelayer struct {
	logger           logging.Logger
	metrics          *ApplicationRelayerMetrics
	network          *peers.AppRequestNetwork
	messageCreator   message.Creator
	responseChan     chan message.InboundMessage
	sourceBlockchain config.SourceBlockchain
	signingSubnetID  ids.ID
	relayerID        database.RelayerID
	warpQuorum       config.WarpQuorum
	db               database.RelayerDatabase
}

func newApplicationRelayer(
	logger logging.Logger,
	metrics *ApplicationRelayerMetrics,
	network *peers.AppRequestNetwork,
	messageCreator message.Creator,
	responseChan chan message.InboundMessage,
	relayerID database.RelayerID,
	db database.RelayerDatabase,
	sourceBlockchain config.SourceBlockchain,
	cfg *config.Config,
) (*applicationRelayer, error) {
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
	return &applicationRelayer{
		logger:           logger,
		metrics:          metrics,
		network:          network,
		messageCreator:   messageCreator,
		responseChan:     responseChan,
		sourceBlockchain: sourceBlockchain,
		relayerID:        relayerID,
		signingSubnetID:  signingSubnet,
		warpQuorum:       quorum,
		db:               db,
	}, nil
}

func (r *applicationRelayer) relayMessage(
	unsignedMessage *avalancheWarp.UnsignedMessage,
	requestID uint32,
	messageManager messages.MessageManager,
	storeProcessedHeight bool,
	blockNumber uint64,
	useAppRequestNetwork bool,
) error {
	shouldSend, err := messageManager.ShouldSendMessage(unsignedMessage, r.relayerID.DestinationBlockchainID)
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

	startCreateSignedMessageTime := time.Now()
	// Query nodes on the origin chain for signatures, and construct the signed warp message.
	var signedMessage *avalancheWarp.Message
	if useAppRequestNetwork {
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

	err = messageManager.SendMessage(signedMessage, r.relayerID.DestinationBlockchainID)
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

	if !storeProcessedHeight {
		return nil
	}

	// Update the database with the latest processed block height
	return r.storeLatestBlockHeight(blockNumber)
}

// createSignedMessage fetches the signed Warp message from the source chain via RPC.
// Each VM may implement their own RPC method to construct the aggregate signature, which
// will need to be accounted for here.
func (r *applicationRelayer) createSignedMessage(unsignedMessage *avalancheWarp.UnsignedMessage) (*avalancheWarp.Message, error) {
	r.logger.Info("Fetching aggregate signature from the source chain validators via API")
	// TODO: To properly support this, we should provide a dedicated Warp API endpoint in the config
	uri := utils.StripFromString(r.sourceBlockchain.RPCEndpoint, "/ext")
	warpClient, err := warpBackend.NewClient(uri, r.sourceBlockchain.GetBlockchainID().String())
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
func (r *applicationRelayer) createSignedMessageAppRequest(unsignedMessage *avalancheWarp.UnsignedMessage, requestID uint32) (*avalancheWarp.Message, error) {
	r.logger.Info("Fetching aggregate signature from the source chain validators via AppRequest")
	connectedValidators, err := r.network.ConnectToCanonicalValidators(r.signingSubnetID)
	if err != nil {
		r.logger.Error(
			"Failed to connect to canonical validators",
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

	// Construct the request

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

	outMsg, err := r.messageCreator.AppRequest(unsignedMessage.SourceChainID, requestID, peers.DefaultAppRequestTimeout, reqBytes)
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
		responsesExpected := len(connectedValidators.ValidatorSet) - len(signatureMap)
		r.logger.Debug(
			"Relayer collecting signatures from peers.",
			zap.Int("attempt", attempt),
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
			)

			// Register a timeout response for each queried node
			reqID := ids.RequestID{
				NodeID:             nodeID,
				SourceChainID:      unsignedMessage.SourceChainID,
				DestinationChainID: unsignedMessage.SourceChainID,
				RequestID:          requestID,
				Op:                 byte(message.AppResponseOp),
			}
			r.network.Handler.RegisterRequest(reqID)
		}

		sentTo := r.network.Network.Send(outMsg, vdrSet, r.sourceBlockchain.GetSubnetID(), subnets.NoOpAllower)
		r.logger.Debug(
			"Sent signature request to network",
			zap.String("messageID", unsignedMessage.ID().String()),
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
			// TODO: In order to run this concurrently, we need to route to each application relayer from the relayer responseChan
			for response := range r.responseChan {
				r.logger.Debug(
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
					if response.Op() == message.AppErrorOp {
						r.logger.Debug("Request timed out")
						return nil, nil
					}

					validator, vdrIndex := connectedValidators.GetValidator(nodeID)
					signature, valid := r.isValidSignatureResponse(unsignedMessage, response, validator.PublicKey)
					if valid {
						r.logger.Debug(
							"Got valid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						signatureMap[vdrIndex] = signature
						accumulatedSignatureWeight.Add(accumulatedSignatureWeight, new(big.Int).SetUint64(validator.Weight))
					} else {
						r.logger.Debug(
							"Got invalid signature response",
							zap.String("nodeID", nodeID.String()),
						)
						return nil, nil
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
								zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
								zap.Error(err),
							)
							return nil, err
						}

						signedMsg, err := avalancheWarp.NewMessage(unsignedMessage, &avalancheWarp.BitSetSignature{
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
		zap.String("destinationBlockchainID", r.relayerID.DestinationBlockchainID.String()),
	)
	return nil, errNotEnoughSignatures
}

// isValidSignatureResponse tries to generate a signature from the peer.AsyncResponse, then verifies the signature against the node's public key.
// If we are unable to generate the signature or verify correctly, false will be returned to indicate no valid signature was found in response.
func (r *applicationRelayer) isValidSignatureResponse(
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
func (r *applicationRelayer) aggregateSignatures(signatureMap map[int]blsSignatureBuf) (*bls.Signature, set.Bits, error) {
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
// Database access
//

// Determines the height to process from. There are three cases:
// 1) The database contains the latest processed block data for the chain
//   - In this case, we return the maximum of the latest processed block and the configured processHistoricalBlocksFromHeight
//
// 2) The database has been configured for the chain, but does not contain the latest processed block data
//   - In this case, we return the configured processHistoricalBlocksFromHeight
//
// 3) The database does not contain any information for the chain.
//   - In this case, we return the configured processHistoricalBlocksFromHeight if it is set, otherwise
//     we return the chain head.
func (r *applicationRelayer) calculateStartingBlockHeight(processHistoricalBlocksFromHeight uint64) (uint64, error) {
	latestProcessedBlock, err := r.getLatestProcessedBlockHeight()
	if database.IsKeyNotFoundError(err) {
		// The database does not contain the latest processed block data for the chain,
		// use the configured process-historical-blocks-from-height instead.
		// If process-historical-blocks-from-height was not configured, start from the chain head.
		if processHistoricalBlocksFromHeight == 0 {
			return r.setProcessedBlockHeightToLatest()
		}
		return processHistoricalBlocksFromHeight, nil
	} else if err != nil {
		// Otherwise, we've encountered an unknown database error
		r.logger.Error(
			"failed to get latest block from database",
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Error(err),
		)
		return 0, err
	}

	// If the database does contain the latest processed block data for the key,
	// use the max of the latest processed block and the configured start block height (if it was provided)
	if latestProcessedBlock > processHistoricalBlocksFromHeight {
		r.logger.Info(
			"Processing historical blocks from the latest processed block in the DB",
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Uint64("latestProcessedBlock", latestProcessedBlock),
		)
		return latestProcessedBlock, nil
	}
	// Otherwise, return the configured start block height
	r.logger.Info(
		"Processing historical blocks from the configured start block height",
		zap.String("relayerID", r.relayerID.ID.String()),
		zap.Uint64("processHistoricalBlocksFromHeight", processHistoricalBlocksFromHeight),
	)
	return processHistoricalBlocksFromHeight, nil
}

// Gets the height of the chain head, writes it to the database, then returns it.
func (r *applicationRelayer) setProcessedBlockHeightToLatest() (uint64, error) {
	ethClient, err := ethclient.DialWithConfig(
		context.Background(),
		r.sourceBlockchain.RPCEndpoint,
		r.sourceBlockchain.HttpHeaders,
		r.sourceBlockchain.QueryParams,
	)
	if err != nil {
		r.logger.Error(
			"Failed to dial node",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.Error(err),
		)
		return 0, err
	}

	latestBlock, err := ethClient.BlockNumber(context.Background())
	if err != nil {
		r.logger.Error(
			"Failed to get latest block",
			zap.String("blockchainID", r.sourceBlockchain.GetBlockchainID().String()),
			zap.Error(err),
		)
		return 0, err
	}

	r.logger.Info(
		"Updating latest processed block in database",
		zap.String("relayerID", r.relayerID.ID.String()),
		zap.Uint64("latestBlock", latestBlock),
	)

	err = r.storeBlockHeight(latestBlock)
	if err != nil {
		r.logger.Error(
			fmt.Sprintf("failed to put %s into database", database.LatestProcessedBlockKey),
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Error(err),
		)
		return 0, err
	}
	return latestBlock, nil
}

// Get the latest processed block height from the database.
// Note that there may be unrelayed messages in the latest processed block
// because it is updated as soon as a single message from that block is relayed,
// and there may be multiple message in the same block.
func (r *applicationRelayer) getLatestProcessedBlockHeight() (uint64, error) {
	latestProcessedBlockData, err := r.db.Get(r.relayerID.ID, database.LatestProcessedBlockKey)
	if err != nil {
		return 0, err
	}
	latestProcessedBlock, err := strconv.ParseUint(string(latestProcessedBlockData), 10, 64)
	if err != nil {
		return 0, err
	}
	return latestProcessedBlock, nil
}

// Store the block height in the database. Does not check against the current latest processed block height.
func (r *applicationRelayer) storeBlockHeight(height uint64) error {
	return r.db.Put(r.relayerID.ID, database.LatestProcessedBlockKey, []byte(strconv.FormatUint(height, 10)))
}

// Stores the block height in the database if it is greater than the current latest processed block height.
func (r *applicationRelayer) storeLatestBlockHeight(height uint64) error {
	// First, check that the stored height is less than the current block height
	// This is necessary because the relayer may be processing blocks out of order on startup
	latestProcessedBlock, err := r.getLatestProcessedBlockHeight()
	if err != nil && !database.IsKeyNotFoundError(err) {
		r.logger.Error(
			"Encountered an unknown error while getting latest processed block from database",
			zap.String("relayerID", r.relayerID.ID.String()),
			zap.Error(err),
		)
		return err
	}

	// An unhandled error at this point indicates the DB does not store the block data for the chain,
	// so we write unconditionally in that case. Otherwise, we only overwrite if the new height is greater
	// than the stored height.
	if err != nil || height > latestProcessedBlock {
		err = r.storeBlockHeight(height)
		if err != nil {
			r.logger.Error(
				fmt.Sprintf("failed to put %s into database", database.LatestProcessedBlockKey),
				zap.String("relayerID", r.relayerID.ID.String()),
				zap.Error(err),
			)
		}
		return err
	}
	return nil
}

//
// Metrics
//

func (r *applicationRelayer) incSuccessfulRelayMessageCount() {
	r.metrics.successfulRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Inc()
}

func (r *applicationRelayer) incFailedRelayMessageCount(failureReason string) {
	r.metrics.failedRelayMessageCount.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String(),
			failureReason).Inc()
}

func (r *applicationRelayer) setCreateSignedMessageLatencyMS(latency float64) {
	r.metrics.createSignedMessageLatencyMS.
		WithLabelValues(
			r.relayerID.DestinationBlockchainID.String(),
			r.sourceBlockchain.GetBlockchainID().String(),
			r.sourceBlockchain.GetSubnetID().String()).Set(latency)
}
