package tests

import (
	"context"
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
	"github.com/ava-labs/subnet-evm/core/types"
	predicateutils "github.com/ava-labs/subnet-evm/predicate"
	subnetevmutils "github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/x/warp"
	teleportermessenger "github.com/ava-labs/teleporter/abi-bindings/go/Teleporter/TeleporterMessenger"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
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
func BasicRelay() {
	var (
		receivedWarpMessage *avalancheWarp.Message
		payload             []byte
		relayerCmd          *exec.Cmd
		relayerCancel       context.CancelFunc
	)

	subnetAInfo := teleporterTestUtils.GetSubnetATestInfo()
	subnetBInfo := teleporterTestUtils.GetSubnetBTestInfo()
	teleporterContractAddress := teleporterTestUtils.GetTeleporterContractAddress()
	fundedAddress, fundedKey := teleporterTestUtils.GetFundedAccountInfo()

	teleporterMessage := teleportermessenger.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           fundedAddress,
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		DestinationAddress:      fundedAddress,
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleportermessenger.TeleporterMessageReceipt{},
		Message:                 []byte{1, 2, 3, 4},
	}

	//
	// Set up relayer config
	//
	hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(subnetAInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(subnetBInfo.ChainNodeURIs[0])
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
		PChainAPIURL:      subnetAInfo.ChainNodeURIs[0],
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
		},
		DestinationSubnets: []config.DestinationSubnet{
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				BlockchainID:      subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				AccountPrivateKey: hex.EncodeToString(fundedKey.D.Bytes()),
			},
		},
	}

	data, err := json.MarshalIndent(relayerConfig, "", "\t")
	Expect(err).Should(BeNil())

	f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
	Expect(err).Should(BeNil())

	_, err = f.Write(data)
	Expect(err).Should(BeNil())
	relayerConfigPath := f.Name()

	log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))

	//
	// Build Relayer
	//
	// Build the awm-relayer binary
	cmd := exec.Command("./scripts/build.sh")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	Expect(err).Should(BeNil())

	//
	// Send a transaction to Subnet A to issue a Warp Message from the Teleporter contract to Subnet B
	//
	log.Info("Sending transaction from Subnet A to Subnet B")
	ctx := context.Background()

	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	log.Info("Packing teleporter message")
	payload, err = teleportermessenger.PackTeleporterMessage(teleporterMessage)
	Expect(err).Should(BeNil())

	input := teleportermessenger.TeleporterMessageInput{
		DestinationBlockchainID: teleporterMessage.DestinationBlockchainID,
		DestinationAddress: teleporterMessage.DestinationAddress,
		FeeInfo: teleportermessenger.TeleporterFeeInfo{
			FeeTokenAddress: fundedAddress,
			Amount:          big.NewInt(0),
		},
		RequiredGasLimit:        teleporterMessage.RequiredGasLimit,
		AllowedRelayerAddresses: teleporterMessage.AllowedRelayerAddresses,
		Message:                 teleporterMessage.Message,
	}

	// Send a transaction to the Teleporter contract
	signedTx := teleporterTestUtils.CreateSendCrossChainMessageTransaction(ctx, subnetAInfo, input, fundedAddress, fundedKey, teleporterContractAddress)

	// Sleep for some time to make sure relayer has started up and subscribed.
	time.Sleep(15 * time.Second)
	log.Info("Subscribing to new heads on destination chain")

	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	log.Info("Sending teleporter transaction", "destinationBlockchainID", subnetBInfo.BlockchainID, "txHash", signedTx.Hash())
	receipt := teleporterTestUtils.SendTransactionAndWaitForAcceptance(ctx, subnetAInfo.ChainWSClient, signedTx)

	bind, err := teleportermessenger.NewTeleporterMessenger(teleporterContractAddress, subnetAInfo.ChainWSClient)
	Expect(err).Should(BeNil())
	sendEvent, err := teleporterTestUtils.GetEventFromLogs(receipt.Logs, bind.ParseSendCrossChainMessage)
	Expect(err).Should(BeNil())
	Expect(sendEvent.DestinationBlockchainID[:]).Should(Equal(subnetBInfo.BlockchainID[:]))

	teleporterMessageID := sendEvent.Message.MessageID

	// Get the latest block from Subnet B
	log.Info("Waiting for new block confirmation")
	newHead := <-newHeadsB
	log.Info("Received new head", "height", newHead.Number.Uint64())
	blockHash := newHead.Hash()
	block, err := subnetBInfo.ChainWSClient.BlockByHash(ctx, blockHash)
	Expect(err).Should(BeNil())
	log.Info(
		"Got block",
		"blockHash", blockHash,
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
	receivedWarpMessage, err = avalancheWarp.ParseMessage(predicateBytes)
	Expect(err).Should(BeNil())

	// Check that the transaction has successful receipt status
	txHash := block.Transactions()[0].Hash()
	receipt, err = subnetBInfo.ChainWSClient.TransactionReceipt(ctx, txHash)
	Expect(err).Should(BeNil())
	Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

	// Check that the transaction emits ReceiveCrossChainMessage
	bind, err = teleportermessenger.NewTeleporterMessenger(teleporterContractAddress, subnetBInfo.ChainWSClient)
	Expect(err).Should(BeNil())

	receiveEvent, err := teleporterTestUtils.GetEventFromLogs(receipt.Logs, bind.ParseReceiveCrossChainMessage)
	Expect(err).Should(BeNil())
	Expect(receiveEvent.OriginBlockchainID[:]).Should(Equal(subnetAInfo.BlockchainID[:]))
	Expect(receiveEvent.Message.MessageID.Uint64()).Should(Equal(teleporterMessageID.Uint64()))

	log.Info("Finished sending warp message, closing down output channel")

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()

	//
	// Validate Received Warp Message Values
	//
	log.Info("Validating received warp message")
	Expect(receivedWarpMessage.SourceChainID).Should(Equal(subnetAInfo.BlockchainID))
	addressedPayload, err := warpPayload.ParseAddressedCall(receivedWarpMessage.Payload)
	Expect(err).Should(BeNil())

	Expect(addressedPayload.Payload).Should(Equal(payload))

	// Check that the teleporter message is correct
	receivedTeleporterMessage, err := teleportermessenger.UnpackTeleporterMessage(addressedPayload.Payload)
	Expect(err).Should(BeNil())
	Expect(*receivedTeleporterMessage).Should(Equal(teleporterMessage))
	receivedDestinationID, err := ids.ToID(receivedTeleporterMessage.DestinationBlockchainID[:])
	Expect(err).Should(BeNil())
	Expect(receivedDestinationID).Should(Equal(subnetBInfo.BlockchainID))

	//
	// Try Relaying Already Delivered Message
	//
	log.Info("Creating new relayer instance to test already delivered message")
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

	// Run the relayer
	relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()
}
