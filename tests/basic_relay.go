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

	teleporterMessage := teleporter.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           fundedAddress,
		DestinationAddress:      fundedAddress,
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleporter.TeleporterMessageReceipt{},
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
				ChainID:           subnetAInfo.BlockchainID.String(),
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
				ChainID:           subnetBInfo.BlockchainID.String(),
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

	nonceA, err := subnetAInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	nonceB, err := subnetBInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	log.Info("Packing teleporter message", "nonceA", nonceA, "nonceB", nonceB)
	payload, err = teleporter.PackSendCrossChainMessageEvent(common.Hash(subnetBInfo.BlockchainID), teleporterMessage)
	Expect(err).Should(BeNil())

	data, err = teleporter.EVMTeleporterContractABI.Pack(
		"sendCrossChainMessage",
		teleporterTestUtils.SendCrossChainMessageInput{
			DestinationChainID: subnetBInfo.BlockchainID,
			DestinationAddress: teleporterMessage.DestinationAddress,
			FeeInfo: teleporterTestUtils.FeeInfo{
				ContractAddress: fundedAddress,
				Amount:          big.NewInt(0),
			},
			RequiredGasLimit:        teleporterMessage.RequiredGasLimit,
			AllowedRelayerAddresses: teleporterMessage.AllowedRelayerAddresses,
			Message:                 teleporterMessage.Message,
		},
	)
	Expect(err).Should(BeNil())

	// Send a transaction to the Teleporter contract
	tx := teleporterTestUtils.NewTestTeleporterTransaction(subnetAInfo.ChainIDInt, teleporterContractAddress, nonceA, data)

	txSigner := types.LatestSignerForChainID(subnetAInfo.ChainIDInt)
	signedTx, err := types.SignTx(tx, txSigner, fundedKey)
	Expect(err).Should(BeNil())

	// Sleep for some time to make sure relayer has started up and subscribed.
	time.Sleep(15 * time.Second)
	log.Info("Subscribing to new heads on destination chain")

	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	log.Info("Sending teleporter transaction", "destinationChainID", subnetBInfo.BlockchainID, "txHash", signedTx.Hash())
	teleporterTestUtils.SendAndWaitForTransaction(ctx, subnetAInfo.ChainWSClient, signedTx)

	receipt, err := subnetAInfo.ChainWSClient.TransactionReceipt(ctx, signedTx.Hash())
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
	packedPredicate := predicateutils.HashSliceToBytes(storageKeyHashes)
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

	//
	// Validate Received Warp Message Values
	//
	log.Info("Validating received warp message")
	Expect(receivedWarpMessage.SourceChainID).Should(Equal(subnetAInfo.BlockchainID))
	addressedPayload, err := warpPayload.ParseAddressedPayload(receivedWarpMessage.Payload)
	Expect(err).Should(BeNil())

	receivedDestinationID, err := ids.ToID(addressedPayload.DestinationChainID.Bytes())
	Expect(err).Should(BeNil())
	Expect(receivedDestinationID).Should(Equal(subnetBInfo.BlockchainID))
	Expect(addressedPayload.DestinationAddress).Should(Equal(teleporterContractAddress))
	Expect(addressedPayload.Payload).Should(Equal(payload))

	// Check that the teleporter message is correct
	receivedTeleporterMessage, err := teleporter.UnpackTeleporterMessage(addressedPayload.Payload)
	Expect(err).Should(BeNil())
	Expect(*receivedTeleporterMessage).Should(Equal(teleporterMessage))
}
