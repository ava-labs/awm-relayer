package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/peers"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/core/types"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"github.com/ava-labs/subnet-evm/x/warp"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	relayerConfigPath string
	teleporterMessage = teleporter.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           teleporterTestUtils.FundedAddress,
		DestinationAddress:      teleporterTestUtils.FundedAddress,
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleporter.TeleporterMessageReceipt{},
		Message:                 []byte{1, 2, 3, 4},
	}
	storageLocation = fmt.Sprintf("%s/.awm-relayer-storage", os.TempDir())
)

// Ginkgo describe node that acts as a container for the relayer e2e tests. This test suite
// will run in order, starting off by setting up the subnet URIs and creating a relayer config
// file. It will then build the relayer binary and run it with the config file. The relayer
// will then send a transaction to the source subnet to issue a Warp message simulting a transaction
// sent from the Teleporter contract. The relayer will then wait for the transaction to be confirmed
// on the destination subnet and verify that the Warp message was received and unpacked correctly.
var _ = ginkgo.Describe("[Relayer E2E]", ginkgo.Ordered, func() {
	var (
		receivedWarpMessage *avalancheWarp.Message
		payload             []byte
		relayerCmd          *exec.Cmd
		relayerCancel       context.CancelFunc
	)

	ginkgo.It("Set up relayer config", ginkgo.Label("Relayer", "Setup Relayer"), func() {
		hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(teleporterTestUtils.ChainANodeURIs[0])
		Expect(err).Should(BeNil())

		hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(teleporterTestUtils.ChainBNodeURIs[0])
		Expect(err).Should(BeNil())

		log.Info(
			"Setting up relayer config",
			"hostA", hostA,
			"portA", portA,
			"blockChainA", teleporterTestUtils.BlockchainIDA.String(),
			"hostB", hostB,
			"portB", portB,
			"blockChainB", teleporterTestUtils.BlockchainIDB.String(),
			"teleporterTestUtils.SubnetA", teleporterTestUtils.SubnetA.String(),
			"teleporterTestUtils.SubnetB", teleporterTestUtils.SubnetB.String(),
		)

		relayerConfig := config.Config{
			LogLevel:          logging.Info.LowerString(),
			NetworkID:         peers.LocalNetworkID,
			PChainAPIURL:      teleporterTestUtils.ChainANodeURIs[0],
			EncryptConnection: false,
			StorageLocation:   storageLocation,
			SourceSubnets: []config.SourceSubnet{
				{
					SubnetID:          teleporterTestUtils.SubnetA.String(),
					ChainID:           teleporterTestUtils.BlockchainIDA.String(),
					VM:                config.EVM.String(),
					EncryptConnection: false,
					APINodeHost:       hostA,
					APINodePort:       portA,
					MessageContracts: map[string]config.MessageProtocolConfig{
						teleporterTestUtils.TeleporterContractAddress.Hex(): {
							MessageFormat: config.TELEPORTER.String(),
							Settings: map[string]interface{}{
								"reward-address": teleporterTestUtils.FundedAddress.Hex(),
							},
						},
					},
				},
			},
			DestinationSubnets: []config.DestinationSubnet{
				{
					SubnetID:          teleporterTestUtils.SubnetB.String(),
					ChainID:           teleporterTestUtils.BlockchainIDB.String(),
					VM:                config.EVM.String(),
					EncryptConnection: false,
					APINodeHost:       hostB,
					APINodePort:       portB,
					AccountPrivateKey: teleporterTestUtils.FundedKeyStr,
				},
			},
		}

		data, err := json.MarshalIndent(relayerConfig, "", "\t")
		Expect(err).Should(BeNil())

		f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
		Expect(err).Should(BeNil())

		_, err = f.Write(data)
		Expect(err).Should(BeNil())
		relayerConfigPath = f.Name()

		log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))
	})

	ginkgo.It("Build Relayer", ginkgo.Label("Relayer", "Build Relayer"), func() {
		// Build the awm-relayer binary
		cmd := exec.Command("./scripts/build.sh")
		out, err := cmd.CombinedOutput()
		fmt.Println(string(out))
		Expect(err).Should(BeNil())
	})

	// Send a transaction to Subnet A to issue a Warp Message from the Teleporter contract to Subnet B
	ginkgo.It("Send Message from A to B", ginkgo.Label("Warp", "SendWarp"), func() {
		ctx := context.Background()

		relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

		nonceA, err := teleporterTestUtils.ChainARPCClient.NonceAt(ctx, teleporterTestUtils.FundedAddress, nil)
		Expect(err).Should(BeNil())

		nonceB, err := teleporterTestUtils.ChainBRPCClient.NonceAt(ctx, teleporterTestUtils.FundedAddress, nil)
		Expect(err).Should(BeNil())

		log.Info("Packing teleporter message", "nonceA", nonceA, "nonceB", nonceB)
		payload, err = teleporter.PackSendCrossChainMessageEvent(common.Hash(teleporterTestUtils.BlockchainIDB), teleporterMessage)
		Expect(err).Should(BeNil())

		data, err := teleporter.EVMTeleporterContractABI.Pack(
			"sendCrossChainMessage",
			teleporterTestUtils.TeleporterMessageInput{
				DestinationChainID: teleporterTestUtils.BlockchainIDB,
				DestinationAddress: teleporterTestUtils.FundedAddress,
				FeeInfo: teleporterTestUtils.FeeInfo{
					ContractAddress: teleporterTestUtils.FundedAddress,
					Amount:          big.NewInt(0),
				},
				RequiredGasLimit:        big.NewInt(1),
				AllowedRelayerAddresses: []common.Address{},
				Message:                 []byte{1, 2, 3, 4},
			},
		)
		Expect(err).Should(BeNil())

		// Send a transaction to the Teleporter contract
		tx := teleporterTestUtils.NewTestTeleporterTransaction(teleporterTestUtils.ChainAIDInt, teleporterTestUtils.TeleporterContractAddress, nonceA, data)

		txSigner := types.LatestSignerForChainID(teleporterTestUtils.ChainAIDInt)
		signedTx, err := types.SignTx(tx, txSigner, teleporterTestUtils.FundedKey)
		Expect(err).Should(BeNil())

		// Sleep for some time to make sure relayer has started up and subscribed.
		time.Sleep(15 * time.Second)
		log.Info("Subscribing to new heads on destination chain")

		newHeadsB := make(chan *types.Header, 10)
		sub, err := teleporterTestUtils.ChainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		Expect(err).Should(BeNil())
		defer sub.Unsubscribe()

		log.Info("Sending teleporter transaction", "destinationChainID", teleporterTestUtils.BlockchainIDB, "txHash", signedTx.Hash())
		err = teleporterTestUtils.ChainARPCClient.SendTransaction(ctx, signedTx)
		Expect(err).Should(BeNil())

		time.Sleep(5 * time.Second)
		receipt, err := teleporterTestUtils.ChainARPCClient.TransactionReceipt(ctx, signedTx.Hash())
		Expect(err).Should(BeNil())
		Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

		sendCrossChainMessageLog := receipt.Logs[0]
		var sendEvent teleporterTestUtils.SendCrossChainMessageEvent
		err = teleporter.EVMTeleporterContractABI.UnpackIntoInterface(&sendEvent, "SendCrossChainMessage", sendCrossChainMessageLog.Data)
		Expect(err).Should(BeNil())
		log.Info(fmt.Sprintf("Sent teleporter message: %#v", sendEvent))
		teleporterMessageID := sendEvent.Message.MessageID

		// Get the latest block from Subnet B
		log.Info("Waiting for new block confirmation")
		newHead := <-newHeadsB
		log.Info("Received new head", "height", newHead.Number.Uint64())
		blockHash := newHead.Hash()
		block, err := teleporterTestUtils.ChainBRPCClient.BlockByHash(ctx, blockHash)
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
		packedPredicate := predicateutils.HashSliceToBytes(storageKeyHashes)
		predicateBytes, err := predicateutils.UnpackPredicate(packedPredicate)
		Expect(err).Should(BeNil())
		receivedWarpMessage, err = avalancheWarp.ParseMessage(predicateBytes)
		Expect(err).Should(BeNil())

		// Check that the transaction has successful receipt status
		txHash := block.Transactions()[0].Hash()
		receipt, err = teleporterTestUtils.ChainBRPCClient.TransactionReceipt(ctx, txHash)
		Expect(err).Should(BeNil())
		Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

		// Check that the transaction emits ReceiveCrossChainMessage
		receiveCrossChainMessageLog := receipt.Logs[0]
		var receiveEvent teleporterTestUtils.ReceiveCrossChainMessageEvent
		err = teleporter.EVMTeleporterContractABI.UnpackIntoInterface(&receiveEvent, "ReceiveCrossChainMessage", receiveCrossChainMessageLog.Data)
		Expect(err).Should(BeNil())
		log.Info(fmt.Sprintf("Received teleporter message: %#v", receiveEvent))
		Expect(receiveEvent.Message.MessageID.Uint64()).Should(Equal(teleporterMessageID.Uint64()))

		log.Info("Finished sending warp message, closing down output channel")

		// Cancel the command and stop the relayer
		relayerCancel()
		_ = relayerCmd.Wait()
	})

	ginkgo.It("Try relaying already delivered message", ginkgo.Label("Relayer", "RelayerAlreadyDeliveredMessage"), func() {
		ctx := context.Background()
		logger := logging.NewLogger(
			"awm-relayer",
			logging.NewWrappedCore(
				logging.Info,
				os.Stdout,
				logging.JSON.ConsoleEncoder(),
			),
		)
		jsonDB, err := database.NewJSONFileStorage(logger, storageLocation, []ids.ID{teleporterTestUtils.BlockchainIDA, teleporterTestUtils.BlockchainIDB})
		Expect(err).Should(BeNil())

		// Modify the JSON database to force the relayer to re-process old blocks
		jsonDB.Put(teleporterTestUtils.BlockchainIDA, []byte(database.LatestProcessedBlockKey), []byte("0"))
		jsonDB.Put(teleporterTestUtils.BlockchainIDB, []byte(database.LatestProcessedBlockKey), []byte("0"))

		// Subscribe to the destination chain block published
		newHeadsB := make(chan *types.Header, 10)
		sub, err := teleporterTestUtils.ChainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		Expect(err).Should(BeNil())
		defer sub.Unsubscribe()

		// Run the relayer
		relayerCmd, relayerCancel = testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

		// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
		Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

		// Cancel the command and stop the relayer
		relayerCancel()
		_ = relayerCmd.Wait()
	})

	ginkgo.It("Validate Received Warp Message Values", ginkgo.Label("Relaery", "VerifyWarp"), func() {
		Expect(receivedWarpMessage.SourceChainID).Should(Equal(teleporterTestUtils.BlockchainIDA))
		addressedPayload, err := warpPayload.ParseAddressedPayload(receivedWarpMessage.Payload)
		Expect(err).Should(BeNil())

		receivedDestinationID, err := ids.ToID(addressedPayload.DestinationChainID.Bytes())
		Expect(err).Should(BeNil())
		Expect(receivedDestinationID).Should(Equal(teleporterTestUtils.BlockchainIDB))
		Expect(addressedPayload.DestinationAddress).Should(Equal(teleporterTestUtils.TeleporterContractAddress))
		Expect(addressedPayload.Payload).Should(Equal(payload))

		// Check that the teleporter message is correct
		receivedTeleporterMessage, err := teleporter.UnpackTeleporterMessage(addressedPayload.Payload)
		Expect(err).Should(BeNil())
		Expect(*receivedTeleporterMessage).Should(Equal(teleporterMessage))
	})
})
