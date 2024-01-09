package tests

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpPayload "github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/peers"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	predicateutils "github.com/ava-labs/subnet-evm/predicate"
	subnetevmutils "github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/x/warp"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

var (
	storageLocation = fmt.Sprintf("%s/.awm-relayer-storage", os.TempDir())
)

// Ginkgo describe node that acts as a container for the relayer e2e tests. This test suite
// will run in order, starting off by setting up the subnet URIs and creating a relayer config
// file. It will then build the relayer binary and run it with the config file. The relayer
// will then send a transaction to the source subnet to issue a Warp message simulting a transaction
// sent from the Teleporter contract. The relayer will then wait for the transaction to be confirmed
// on the destination subnet and verify that the Warp message was received and unpacked correctly.
func BasicRelay(network interfaces.LocalNetwork) {
	var (
		relayerCmd    *exec.Cmd
		relayerCancel context.CancelFunc
	)
	subnetAInfo := network.GetPrimaryNetworkInfo()
	subnetBInfo, _ := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()

	//
	// Fund the relayer address on all subnets
	//
	ctx := context.Background()

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	relayerAddress := crypto.PubkeyToAddress(relayerKey.PublicKey)

	fundAmount := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth
	fundRelayerTxA := utils.CreateNativeTransferTransaction(
		ctx, subnetAInfo, fundedKey, relayerAddress, fundAmount,
	)
	utils.SendTransactionAndWaitForSuccess(ctx, subnetAInfo, fundRelayerTxA)
	fundRelayerTxB := utils.CreateNativeTransferTransaction(
		ctx, subnetBInfo, fundedKey, relayerAddress, fundAmount,
	)
	utils.SendTransactionAndWaitForSuccess(ctx, subnetBInfo, fundRelayerTxB)

	//
	// Set up relayer config
	//
	hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(subnetAInfo.NodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(subnetBInfo.NodeURIs[0])
	Expect(err).Should(BeNil())

	log.Info(
		"Setting up relayer config",
		"hostA", hostA,
		"portA", portA,
		"blockChainA", subnetAInfo.BlockchainID.String(),
		"hostB", hostB,
		"portB", portB,
		"blockChainB", subnetBInfo.BlockchainID.String(),
		"subnetA", subnetAInfo.SubnetID.String(),
		"subnetB", subnetBInfo.SubnetID.String(),
	)

	relayerConfig := config.Config{
		LogLevel:          logging.Info.LowerString(),
		NetworkID:         peers.LocalNetworkID,
		PChainAPIURL:      subnetAInfo.NodeURIs[0],
		EncryptConnection: false,
		StorageLocation:   storageLocation,
		SourceSubnets: []config.SourceSubnet{
			{
				SubnetID:          subnetAInfo.SubnetID.String(),
				BlockchainID:      subnetAInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostA,
				APINodePort:       portA,
				MessageContracts: map[string]config.MessageProtocolConfig{
					teleporterContractAddress.Hex(): {
						MessageFormat: config.TELEPORTER.String(),
						Settings: map[string]interface{}{
							"reward-address": fundedAddress.Hex(),
						},
					},
				},
			},
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				BlockchainID:      subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				MessageContracts: map[string]config.MessageProtocolConfig{
					teleporterContractAddress.Hex(): {
						MessageFormat: config.TELEPORTER.String(),
						Settings: map[string]interface{}{
							"reward-address": fundedAddress.Hex(),
						},
					},
				},
			},
		},
		DestinationSubnets: []config.DestinationSubnet{
			{
				SubnetID:          subnetAInfo.SubnetID.String(),
				BlockchainID:      subnetAInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostA,
				APINodePort:       portA,
				AccountPrivateKey: hex.EncodeToString(relayerKey.D.Bytes()),
			},
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				BlockchainID:      subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				AccountPrivateKey: hex.EncodeToString(relayerKey.D.Bytes()),
			},
		},
	}

	relayerConfigPath := writeRelayerConfig(relayerConfig)

	//
	// Build Relayer
	//
	// Build the awm-relayer binary
	cmd := exec.Command("./scripts/build.sh")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	Expect(err).Should(BeNil())

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayer")
	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayer to start up")
	time.Sleep(15 * time.Second)

	log.Info("Sending transaction from Subnet A to Subnet B")
	relayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
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
		fundedKey,
		fundedAddress,
	)

	log.Info("Finished sending warp message, closing down output channel")
	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()

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
	jsonDB, err := database.NewJSONFileStorage(logger, storageLocation, []ids.ID{subnetAInfo.BlockchainID, subnetBInfo.BlockchainID})
	Expect(err).Should(BeNil())

	// Modify the JSON database to force the relayer to re-process old blocks
	jsonDB.Put(subnetAInfo.BlockchainID, []byte(database.LatestProcessedBlockKey), []byte("0"))
	jsonDB.Put(subnetBInfo.BlockchainID, []byte(database.LatestProcessedBlockKey), []byte("0"))

	// Subscribe to the destination chain
	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	// Run the relayer
	log.Info("Creating new relayer instance to test already delivered message")
	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()

	//
	// Set StartBlockHeight in config
	//
	log.Info("Test Setting StartBlockHeight in config")

	// Send three Teleporter messages from subnet A to subnet B
	log.Info("Sending three Teleporter messages from subnet A to subnet B")
	_, id1 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	_, id2 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	_, id3 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)

	currHeight, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	log.Info("Current block height", "height", currHeight)

	// Configure the relayer such that it will only process the last of the three messages sent above.
	// The relayer DB stores the height of the block *before* the first message, so by setting the
	// StartBlockHeight to the block height of the *third* message, we expect the relayer to skip
	// the first two messages on startup, but process the third.
	modifiedRelayerConfig := relayerConfig
	modifiedRelayerConfig.SourceSubnets[0].StartBlockHeight = int64(currHeight)
	modifiedRelayerConfig.ProcessMissedBlocks = true
	relayerConfigPath = writeRelayerConfig(modifiedRelayerConfig)

	log.Info("Starting the relayer")
	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)
	log.Info("Waiting for a new block confirmation on subnet B")
	<-newHeadsB
	delivered1, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, subnetAInfo.BlockchainID, id1,
	)
	Expect(err).Should(BeNil())
	delivered2, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, subnetAInfo.BlockchainID, id2,
	)
	Expect(err).Should(BeNil())
	delivered3, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, subnetAInfo.BlockchainID, id3,
	)
	Expect(err).Should(BeNil())
	Expect(delivered1).Should(BeFalse())
	Expect(delivered2).Should(BeFalse())
	Expect(delivered3).Should(BeTrue())

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()
}

func sendBasicTeleporterMessage(
	ctx context.Context,
	source interfaces.SubnetTestInfo,
	destination interfaces.SubnetTestInfo,
	fundedKey *ecdsa.PrivateKey,
	fundedAddress common.Address,
) (teleportermessenger.TeleporterMessage, *big.Int) {
	log.Info("Packing Teleporter message")
	teleporterMessage := teleportermessenger.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           fundedAddress,
		DestinationBlockchainID: destination.BlockchainID,
		DestinationAddress:      fundedAddress,
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleportermessenger.TeleporterMessageReceipt{},
		Message:                 []byte{1, 2, 3, 4},
	}

	input := teleportermessenger.TeleporterMessageInput{
		DestinationBlockchainID: teleporterMessage.DestinationBlockchainID,
		DestinationAddress:      teleporterMessage.DestinationAddress,
		FeeInfo: teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: fundedAddress,
			Amount:          big.NewInt(0),
		},
		RequiredGasLimit:        teleporterMessage.RequiredGasLimit,
		AllowedRelayerAddresses: teleporterMessage.AllowedRelayerAddresses,
		Message:                 teleporterMessage.Message,
	}

	// Send a transaction to the Teleporter contract
	log.Info(
		"Sending teleporter transaction",
		"sourceBlockchainID", source.BlockchainID,
		"destinationBlockchainID", destination.BlockchainID,
	)
	_, teleporterMessageID := teleporterTestUtils.SendCrossChainMessageAndWaitForAcceptance(
		ctx,
		source,
		destination,
		input,
		fundedKey,
	)

	return teleporterMessage, teleporterMessageID
}

func relayBasicMessage(
	ctx context.Context,
	source interfaces.SubnetTestInfo,
	destination interfaces.SubnetTestInfo,
	fundedKey *ecdsa.PrivateKey,
	fundedAddress common.Address,
) {
	newHeadsDest := make(chan *types.Header, 10)
	sub, err := destination.WSClient.SubscribeNewHead(ctx, newHeadsDest)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	teleporterMessage, teleporterMessageID := sendBasicTeleporterMessage(
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
	Expect(receiveEvent.OriginBlockchainID[:]).Should(Equal(source.BlockchainID[:]))
	Expect(receiveEvent.Message.MessageID.Uint64()).Should(Equal(teleporterMessageID.Uint64()))

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

	Expect(receivedTeleporterMessage.MessageID.Uint64()).Should(Equal(teleporterMessageID.Uint64()))
	Expect(receivedTeleporterMessage.SenderAddress).Should(Equal(teleporterMessage.SenderAddress))
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
