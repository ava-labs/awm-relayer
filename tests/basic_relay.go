package tests

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	predicateutils "github.com/ava-labs/subnet-evm/predicate"
	subnetevmutils "github.com/ava-labs/subnet-evm/utils"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	teleporterUtils "github.com/ava-labs/teleporter/utils/teleporter-utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// This tests the basic functionality of the relayer, including:
// - Relaying from Subnet A to Subnet B
// - Relaying from Subnet B to Subnet A
// - Relaying an already delivered message
// - Setting ProcessHistoricalBlocksFromHeight in config
func BasicRelay(network interfaces.LocalNetwork) {
	subnetAInfo := network.GetPrimaryNetworkInfo()
	subnetBInfo, _ := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Fund the relayer address on all subnets
	//
	ctx := context.Background()

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey)

	//
	// Set up relayer config
	//
	relayerConfig := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)

	relayerConfigPath := writeRelayerConfig(relayerConfig)

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayer")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayer to start up")
	time.Sleep(15 * time.Second)

	log.Info("Sending transaction from Subnet A to Subnet B")
	relayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)

	//
	// Test Relaying from Subnet B to Subnet A
	//
	log.Info("Test Relaying from Subnet B to Subnet A")
	relayBasicMessage(
		ctx,
		subnetBInfo,
		subnetAInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)

	log.Info("Finished sending warp message, closing down output channel")
	// Cancel the command and stop the relayer
	relayerCleanup()

	//
	// Try Relaying Already Delivered Message
	//
	log.Info("Test Relaying Already Delivered Message")
	logger := logging.NewLogger(
		"awm-relayer",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)
	jsonDB, err := database.NewJSONFileStorage(logger, relayerConfig.StorageLocation, database.GetConfigRelayerKeys(&relayerConfig))
	Expect(err).Should(BeNil())

	relayerKeyA := database.CalculateRelayerKey(subnetAInfo.BlockchainID, subnetBInfo.BlockchainID, common.Address{}, common.Address{})
	relayerKeyB := database.CalculateRelayerKey(subnetBInfo.BlockchainID, subnetAInfo.BlockchainID, common.Address{}, common.Address{})
	// Modify the JSON database to force the relayer to re-process old blocks
	jsonDB.Put(relayerKeyA, []byte(database.LatestProcessedBlockKey), []byte("0"))
	jsonDB.Put(relayerKeyB, []byte(database.LatestProcessedBlockKey), []byte("0"))

	// Subscribe to the destination chain
	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	// Run the relayer
	log.Info("Creating new relayer instance to test already delivered message")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	// Cancel the command and stop the relayer
	relayerCleanup()

	//
	// Set ProcessHistoricalBlocksFromHeight in config
	//
	log.Info("Test Setting ProcessHistoricalBlocksFromHeight in config")

	// Send three Teleporter messages from subnet A to subnet B
	log.Info("Sending three Teleporter messages from subnet A to subnet B")
	_, _, id1 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	_, _, id2 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	_, _, id3 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)

	currHeight, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	log.Info("Current block height", "height", currHeight)

	// Configure the relayer such that it will only process the last of the three messages sent above.
	// The relayer DB stores the height of the block *before* the first message, so by setting the
	// ProcessHistoricalBlocksFromHeight to the block height of the *third* message, we expect the relayer to skip
	// the first two messages on startup, but process the third.
	modifiedRelayerConfig := relayerConfig
	modifiedRelayerConfig.SourceBlockchains[0].ProcessHistoricalBlocksFromHeight = currHeight
	modifiedRelayerConfig.ProcessMissedBlocks = true
	relayerConfigPath = writeRelayerConfig(modifiedRelayerConfig)

	log.Info("Starting the relayer")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()
	log.Info("Waiting for a new block confirmation on subnet B")
	<-newHeadsB
	delivered1, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, id1,
	)
	Expect(err).Should(BeNil())
	delivered2, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, id2,
	)
	Expect(err).Should(BeNil())
	delivered3, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, id3,
	)
	Expect(err).Should(BeNil())
	Expect(delivered1).Should(BeFalse())
	Expect(delivered2).Should(BeFalse())
	Expect(delivered3).Should(BeTrue())
}

func sendBasicTeleporterMessage(
	ctx context.Context,
	source interfaces.SubnetTestInfo,
	destination interfaces.SubnetTestInfo,
	fundedKey *ecdsa.PrivateKey,
	fundedAddress common.Address,
) (*types.Receipt, teleportermessenger.TeleporterMessage, ids.ID) {
	input := teleportermessenger.TeleporterMessageInput{
		DestinationBlockchainID: destination.BlockchainID,
		DestinationAddress:      fundedAddress,
		FeeInfo: teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: fundedAddress,
			Amount:          big.NewInt(0),
		},
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Message:                 []byte{1, 2, 3, 4},
	}

	// Send a transaction to the Teleporter contract
	log.Info(
		"Sending teleporter transaction",
		"sourceBlockchainID", source.BlockchainID,
		"destinationBlockchainID", destination.BlockchainID,
	)
	receipt, teleporterMessageID := teleporterTestUtils.SendCrossChainMessageAndWaitForAcceptance(
		ctx,
		source,
		destination,
		input,
		fundedKey,
	)
	sendEvent, err := teleporterTestUtils.GetEventFromLogs(receipt.Logs, source.TeleporterMessenger.ParseSendCrossChainMessage)
	Expect(err).Should(BeNil())

	return receipt, sendEvent.Message, teleporterMessageID
}

func relayBasicMessage(
	ctx context.Context,
	source interfaces.SubnetTestInfo,
	destination interfaces.SubnetTestInfo,
	teleporterContractAddress common.Address,
	fundedKey *ecdsa.PrivateKey,
	fundedAddress common.Address,
) {
	newHeadsDest := make(chan *types.Header, 10)
	sub, err := destination.WSClient.SubscribeNewHead(ctx, newHeadsDest)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	_, teleporterMessage, teleporterMessageID := sendBasicTeleporterMessage(
		ctx,
		source,
		destination,
		fundedKey,
		fundedAddress,
	)

	log.Info("Waiting for new block confirmation")
	newHead := <-newHeadsDest
	blockNumber := newHead.Number
	log.Info(
		"Received new head",
		"height", blockNumber.Uint64(),
		"hash", newHead.Hash(),
	)
	block, err := destination.RPCClient.BlockByNumber(ctx, blockNumber)
	Expect(err).Should(BeNil())
	log.Info(
		"Got block",
		"blockHash", block.Hash(),
		"blockNumber", block.NumberU64(),
		"transactions", block.Transactions(),
		"numTransactions", len(block.Transactions()),
		"block", block,
	)
	accessLists := block.Transactions()[0].AccessList()
	Expect(len(accessLists)).Should(Equal(1))
	Expect(accessLists[0].Address).Should(Equal(warp.Module.Address))

	// Check the transaction storage key has warp message we're expecting
	storageKeyHashes := accessLists[0].StorageKeys
	packedPredicate := subnetevmutils.HashSliceToBytes(storageKeyHashes)
	predicateBytes, err := predicateutils.UnpackPredicate(packedPredicate)
	Expect(err).Should(BeNil())
	receivedWarpMessage, err := avalancheWarp.ParseMessage(predicateBytes)
	Expect(err).Should(BeNil())

	// Check that the transaction has successful receipt status
	txHash := block.Transactions()[0].Hash()
	receipt, err := destination.RPCClient.TransactionReceipt(ctx, txHash)
	Expect(err).Should(BeNil())
	Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

	// Check that the transaction emits ReceiveCrossChainMessage
	receiveEvent, err := teleporterTestUtils.GetEventFromLogs(receipt.Logs, destination.TeleporterMessenger.ParseReceiveCrossChainMessage)
	Expect(err).Should(BeNil())
	Expect(receiveEvent.SourceBlockchainID[:]).Should(Equal(source.BlockchainID[:]))
	Expect(receiveEvent.MessageID[:]).Should(Equal(teleporterMessageID[:]))

	//
	// Validate Received Warp Message Values
	//
	log.Info("Validating received warp message")
	Expect(receivedWarpMessage.SourceChainID).Should(Equal(source.BlockchainID))
	addressedPayload, err := warpPayload.ParseAddressedCall(receivedWarpMessage.Payload)
	Expect(err).Should(BeNil())

	// Check that the teleporter message is correct
	// We don't validate the entire message, since the message receipts
	// are populated by the Teleporter contract
	receivedTeleporterMessage, err := teleportermessenger.UnpackTeleporterMessage(addressedPayload.Payload)
	Expect(err).Should(BeNil())

	receivedMessageID, err := teleporterUtils.CalculateMessageID(teleporterContractAddress, source.BlockchainID, destination.BlockchainID, teleporterMessage.MessageNonce)
	Expect(err).Should(BeNil())
	Expect(receivedMessageID).Should(Equal(teleporterMessageID))
	Expect(receivedTeleporterMessage.OriginSenderAddress).Should(Equal(teleporterMessage.OriginSenderAddress))
	receivedDestinationID, err := ids.ToID(receivedTeleporterMessage.DestinationBlockchainID[:])
	Expect(err).Should(BeNil())
	Expect(receivedDestinationID).Should(Equal(destination.BlockchainID))
	Expect(receivedTeleporterMessage.DestinationAddress).Should(Equal(teleporterMessage.DestinationAddress))
	Expect(receivedTeleporterMessage.RequiredGasLimit.Uint64()).Should(Equal(teleporterMessage.RequiredGasLimit.Uint64()))
	Expect(receivedTeleporterMessage.Message).Should(Equal(teleporterMessage.Message))
}

func writeRelayerConfig(relayerConfig config.Config) string {
	data, err := json.MarshalIndent(relayerConfig, "", "\t")
	Expect(err).Should(BeNil())

	f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
	Expect(err).Should(BeNil())

	_, err = f.Write(data)
	Expect(err).Should(BeNil())
	relayerConfigPath := f.Name()

	log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))
	return relayerConfigPath
}
