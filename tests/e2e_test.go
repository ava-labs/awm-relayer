// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/peers"
	relayerEvm "github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/tests/utils/runner"
	teleporter_block_hash "github.com/ava-labs/teleporter/abis/go/teleporter-block-hash"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	fundedKeyStr    = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
	warpGenesisFile = "./tests/warp-genesis.json"
)

var (
	anrConfig                 = runner.NewDefaultANRConfig()
	manager                   = runner.NewNetworkManager(anrConfig)
	fundedAddress             = common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	warpChainConfigPath       string
	relayerConfigPath         string
	teleporterContractAddress common.Address
	teleporterMessage         = teleporter.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           fundedAddress,
		DestinationAddress:      fundedAddress,
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleporter.TeleporterMessageReceipt{},
		Message:                 []byte{1, 2, 3, 4},
	}
	storageLocation                  = fmt.Sprintf("%s/.awm-relayer-storage", os.TempDir())
	subnetIDs                        []ids.ID
	subnetA, subnetB                 ids.ID
	fundedKey                        *ecdsa.PrivateKey
	chainAWSClient, chainBWSClient   ethclient.Client
	chainARPCClient, chainBRPCClient ethclient.Client
	chainARPCURI, chainBRPCURI       string
	chainAIDInt, chainBIDInt         *big.Int
	blockchainIDA, blockchainIDB     ids.ID
	chainANodeURIs, chainBNodeURIs   []string
)

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Relayer e2e test")
}

// BeforeSuite starts the default network and adds 10 new nodes as validators with BLS keys
// registered on the P-Chain.
// Adds two disjoint sets of 5 of the new validator nodes to validate two new subnets with a
// a single Subnet-EVM blockchain.
var _ = ginkgo.BeforeSuite(func() {
	ctx := context.Background()
	var err error

	// Name 10 new validators (which should have BLS key registered)
	subnetANodeNames := []string{}
	subnetBNodeNames := []string{}
	for i := 1; i <= 10; i++ {
		n := fmt.Sprintf("node%d-bls", i)
		if i <= 5 {
			subnetANodeNames = append(subnetANodeNames, n)
		} else {
			subnetBNodeNames = append(subnetBNodeNames, n)
		}
	}
	f, err := os.CreateTemp(os.TempDir(), "config.json")
	Expect(err).Should(BeNil())
	_, err = f.Write([]byte(`{"warp-api-enabled": true}`))
	Expect(err).Should(BeNil())
	warpChainConfigPath = f.Name()

	// Make sure that the warp genesis file exists
	_, err = os.Stat(warpGenesisFile)
	Expect(err).Should(BeNil())

	// Construct the network using the avalanche-network-runner
	_, err = manager.StartDefaultNetwork(ctx)
	Expect(err).Should(BeNil())
	err = manager.SetupNetwork(
		ctx,
		anrConfig.AvalancheGoExecPath,
		[]*rpcpb.BlockchainSpec{
			{
				VmName:      evm.IDStr,
				Genesis:     warpGenesisFile,
				ChainConfig: warpChainConfigPath,
				SubnetSpec: &rpcpb.SubnetSpec{
					SubnetConfig: "",
					Participants: subnetANodeNames,
				},
			},
			{
				VmName:      evm.IDStr,
				Genesis:     warpGenesisFile,
				ChainConfig: warpChainConfigPath,
				SubnetSpec: &rpcpb.SubnetSpec{
					SubnetConfig: "",
					Participants: subnetBNodeNames,
				},
			},
		},
	)
	Expect(err).Should(BeNil())

	// Issue transactions to activate the proposerVM fork on the receiving chain
	fundedKey, err = crypto.HexToECDSA(fundedKeyStr)
	Expect(err).Should(BeNil())
	setUpProposerVm(ctx, fundedKey, manager, 1)

	// Setup subnet URIs
	subnetIDs = manager.GetSubnets()
	Expect(len(subnetIDs)).Should(Equal(2))

	subnetA = subnetIDs[0]
	subnetADetails, ok := manager.GetSubnet(subnetA)
	Expect(ok).Should(BeTrue())
	Expect(len(subnetADetails.ValidatorURIs)).Should(Equal(5))
	blockchainIDA = subnetADetails.BlockchainID
	chainANodeURIs = append(chainANodeURIs, subnetADetails.ValidatorURIs...)

	subnetB = subnetIDs[1]
	subnetBDetails, ok := manager.GetSubnet(subnetB)
	Expect(ok).Should(BeTrue())
	Expect(len(subnetBDetails.ValidatorURIs)).Should(Equal(5))
	blockchainIDB = subnetBDetails.BlockchainID
	chainBNodeURIs = append(chainBNodeURIs, subnetBDetails.ValidatorURIs...)

	log.Info("Created URIs for both subnets", "ChainAURIs", chainANodeURIs, "ChainBURIs", chainBNodeURIs, "blockchainIDA", blockchainIDA, "blockchainIDB", blockchainIDB)
	log.Info("subnet ID", "subnetA", subnetADetails.SubnetID, "subnetB", subnetBDetails.SubnetID)

	chainAWSURI := httpToWebsocketURI(chainANodeURIs[0], blockchainIDA.String())
	chainARPCURI = httpToRPCURI(chainANodeURIs[0], blockchainIDA.String())
	log.Info("Creating ethclient for blockchainA", "wsURI", chainAWSURI, "rpcURL, chainARPCURI")
	chainAWSClient, err = ethclient.Dial(chainAWSURI)
	Expect(err).Should(BeNil())
	chainARPCClient, err = ethclient.Dial(chainARPCURI)
	Expect(err).Should(BeNil())

	chainAIDInt, err = chainARPCClient.ChainID(context.Background())
	Expect(err).Should(BeNil())

	chainBWSURI := httpToWebsocketURI(chainBNodeURIs[0], blockchainIDB.String())
	chainBRPCURI = httpToRPCURI(chainBNodeURIs[0], blockchainIDB.String())
	log.Info("Creating ethclient for blockchainB", "wsURI", chainBWSURI)
	chainBWSClient, err = ethclient.Dial(chainBWSURI)
	Expect(err).Should(BeNil())
	chainBRPCClient, err = ethclient.Dial(chainBRPCURI)
	Expect(err).Should(BeNil())

	chainBIDInt, err = chainBRPCClient.ChainID(context.Background())
	Expect(err).Should(BeNil())
	log.Info("Finished setting up e2e test subnet variables")

	// log.Info("Deploying Teleporter contract to subnets")
	// // Read in the Teleporter contract information
	// teleporterContractAddress = common.HexToAddress(readHexTextFile("./tests/UniversalTeleporterMessengerContractAddress.txt"))
	// teleporterDeployerAddress := common.HexToAddress(readHexTextFile("./tests/UniversalTeleporterDeployerAddress.txt"))
	// teleporterDeployerTransaction := readHexTextFile("./tests/UniversalTeleporterDeployerTransaction.txt")

	// nonceA, err := chainARPCClient.NonceAt(ctx, fundedAddress, nil)
	// Expect(err).Should(BeNil())

	// nonceB, err := chainBRPCClient.NonceAt(ctx, fundedAddress, nil)
	// Expect(err).Should(BeNil())

	// gasTipCapA, err := chainARPCClient.SuggestGasTipCap(context.Background())
	// Expect(err).Should(BeNil())
	// gasTipCapB, err := chainBRPCClient.SuggestGasTipCap(context.Background())
	// Expect(err).Should(BeNil())

	// baseFeeA, err := chainARPCClient.EstimateBaseFee(context.Background())
	// Expect(err).Should(BeNil())
	// gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
	// gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

	// baseFeeB, err := chainBRPCClient.EstimateBaseFee(context.Background())
	// Expect(err).Should(BeNil())
	// gasFeeCapB := baseFeeB.Mul(baseFeeB, big.NewInt(relayerEvm.BaseFeeFactor))
	// gasFeeCapB.Add(gasFeeCapB, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

	// // Fund the deployer address
	// {
	// 	value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth
	// 	txA := types.NewTx(&types.DynamicFeeTx{
	// 		ChainID:   chainAIDInt,
	// 		Nonce:     nonceA,
	// 		To:        &teleporterDeployerAddress,
	// 		Gas:       defaultTeleporterMessageGas,
	// 		GasFeeCap: gasFeeCapA,
	// 		GasTipCap: gasTipCapA,
	// 		Value:     value,
	// 	})
	// 	txSignerA := types.LatestSignerForChainID(chainAIDInt)
	// 	triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
	// 	Expect(err).Should(BeNil())
	// 	err = chainARPCClient.SendTransaction(ctx, triggerTxA)
	// 	Expect(err).Should(BeNil())
	// 	time.Sleep(5 * time.Second)
	// 	receipt, err := chainARPCClient.TransactionReceipt(ctx, triggerTxA.Hash())
	// 	Expect(err).Should(BeNil())
	// 	Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))
	// }
	// {
	// 	value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth
	// 	txB := types.NewTx(&types.DynamicFeeTx{
	// 		ChainID:   chainBIDInt,
	// 		Nonce:     nonceB,
	// 		To:        &teleporterDeployerAddress,
	// 		Gas:       defaultTeleporterMessageGas,
	// 		GasFeeCap: gasFeeCapB,
	// 		GasTipCap: gasTipCapB,
	// 		Value:     value,
	// 	})
	// 	txSignerB := types.LatestSignerForChainID(chainBIDInt)
	// 	triggerTxB, err := types.SignTx(txB, txSignerB, fundedKey)
	// 	Expect(err).Should(BeNil())
	// 	err = chainBRPCClient.SendTransaction(ctx, triggerTxB)
	// 	Expect(err).Should(BeNil())
	// 	time.Sleep(5 * time.Second)
	// 	receipt, err := chainBRPCClient.TransactionReceipt(ctx, triggerTxB.Hash())
	// 	Expect(err).Should(BeNil())
	// 	Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))
	// }
	// // Deploy Teleporter on the two subnets
	// {
	// 	rpcClient, err := rpc.DialContext(ctx, chainARPCURI)
	// 	Expect(err).Should(BeNil())
	// 	err = rpcClient.CallContext(ctx, nil, "eth_sendRawTransaction", teleporterDeployerTransaction)
	// 	Expect(err).Should(BeNil())
	// 	time.Sleep(5 * time.Second)
	// 	teleporterCode, err := chainARPCClient.CodeAt(ctx, teleporterContractAddress, nil)
	// 	Expect(err).Should(BeNil())
	// 	Expect(len(teleporterCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode
	// }
	// {
	// 	rpcClient, err := rpc.DialContext(ctx, chainBRPCURI)
	// 	Expect(err).Should(BeNil())
	// 	err = rpcClient.CallContext(ctx, nil, "eth_sendRawTransaction", teleporterDeployerTransaction)
	// 	Expect(err).Should(BeNil())
	// 	time.Sleep(5 * time.Second)
	// 	teleporterCode, err := chainBRPCClient.CodeAt(ctx, teleporterContractAddress, nil)
	// 	Expect(err).Should(BeNil())
	// 	Expect(len(teleporterCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode
	// }
	// log.Info("Finished deploying Teleporter contracts")

	log.Info("Set up ginkgo before suite")
})

var _ = ginkgo.AfterSuite(func() {
	log.Info("Running ginkgo after suite")
	Expect(manager).ShouldNot(BeNil())
	Expect(manager.TeardownNetwork()).Should(BeNil())
	Expect(os.Remove(warpChainConfigPath)).Should(BeNil())
	Expect(os.Remove(relayerConfigPath)).Should(BeNil())
})

var _ = ginkgo.Describe("[AWM Relayer Integration Tests", func() {
	ginkgo.It("Publish Block Hashes", PublishBlockHashes)
})

// Ginkgo describe node that acts as a container for the relayer e2e tests. This test suite
// will run in order, starting off by setting up the subnet URIs and creating a relayer config
// file. It will then build the relayer binary and run it with the config file. The relayer
// will then send a transaction to the source subnet to issue a Warp message simulting a transaction
// sent from the Teleporter contract. The relayer will then wait for the transaction to be confirmed
// on the destination subnet and verify that the Warp message was received and unpacked correctly.
// var _ = ginkgo.Describe("[Relayer E2E]", ginkgo.Ordered, func() {
// 	var (
// 		receivedWarpMessage *avalancheWarp.Message
// 		payload             []byte
// 		relayerCmd          *exec.Cmd
// 		relayerCancel       context.CancelFunc
// 	)

// 	ginkgo.It("Set up relayer config", ginkgo.Label("Relayer", "Setup Relayer"), func() {
// 		hostA, portA, err := getURIHostAndPort(chainANodeURIs[0])
// 		Expect(err).Should(BeNil())

// 		hostB, portB, err := getURIHostAndPort(chainBNodeURIs[0])
// 		Expect(err).Should(BeNil())

// 		log.Info(
// 			"Setting up relayer config",
// 			"hostA", hostA,
// 			"portA", portA,
// 			"blockChainA", blockchainIDA.String(),
// 			"hostB", hostB,
// 			"portB", portB,
// 			"blockChainB", blockchainIDB.String(),
// 			"subnetA", subnetA.String(),
// 			"subnetB", subnetB.String(),
// 		)

// 		relayerConfig := config.Config{
// 			LogLevel:          logging.Info.LowerString(),
// 			NetworkID:         peers.LocalNetworkID,
// 			PChainAPIURL:      chainANodeURIs[0],
// 			EncryptConnection: false,
// 			StorageLocation:   storageLocation,
// 			SourceSubnets: []config.SourceSubnet{
// 				{
// 					SubnetID:          subnetA.String(),
// 					ChainID:           blockchainIDA.String(),
// 					VM:                config.EVM.String(),
// 					EncryptConnection: false,
// 					APINodeHost:       hostA,
// 					APINodePort:       portA,
// 					MessageContracts: map[string]config.MessageProtocolConfig{
// 						teleporterContractAddress.Hex(): {
// 							MessageFormat: config.TELEPORTER.String(),
// 							Settings: map[string]interface{}{
// 								"reward-address": fundedAddress.Hex(),
// 							},
// 						},
// 					},
// 				},
// 			},
// 			DestinationSubnets: []config.DestinationSubnet{
// 				{
// 					SubnetID:          subnetB.String(),
// 					ChainID:           blockchainIDB.String(),
// 					VM:                config.EVM.String(),
// 					EncryptConnection: false,
// 					APINodeHost:       hostB,
// 					APINodePort:       portB,
// 					AccountPrivateKey: fundedKeyStr,
// 				},
// 			},
// 		}

// 		data, err := json.MarshalIndent(relayerConfig, "", "\t")
// 		Expect(err).Should(BeNil())

// 		f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
// 		Expect(err).Should(BeNil())

// 		_, err = f.Write(data)
// 		Expect(err).Should(BeNil())
// 		relayerConfigPath = f.Name()

// 		log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))
// 	})

// 	ginkgo.It("Build Relayer", ginkgo.Label("Relayer", "Build Relayer"), func() {
// 		// Build the awm-relayer binary
// 		cmd := exec.Command("./scripts/build.sh")
// 		out, err := cmd.CombinedOutput()
// 		fmt.Println(string(out))
// 		Expect(err).Should(BeNil())
// 	})

// 	// Send a transaction to Subnet A to issue a Warp Message from the Teleporter contract to Subnet B
// 	ginkgo.It("Send Message from A to B", ginkgo.Label("Warp", "SendWarp"), func() {
// 		ctx := context.Background()

// 		relayerCmd, relayerCancel = runRelayerExecutable(ctx)

// 		nonceA, err := chainARPCClient.NonceAt(ctx, fundedAddress, nil)
// 		Expect(err).Should(BeNil())

// 		nonceB, err := chainBRPCClient.NonceAt(ctx, fundedAddress, nil)
// 		Expect(err).Should(BeNil())

// 		log.Info("Packing teleporter message", "nonceA", nonceA, "nonceB", nonceB)
// 		payload, err = teleporter.PackSendCrossChainMessageEvent(common.Hash(blockchainIDB), teleporterMessage)
// 		Expect(err).Should(BeNil())

// 		data, err := teleporter.EVMTeleporterContractABI.Pack(
// 			"sendCrossChainMessage",
// 			TeleporterMessageInput{
// 				DestinationChainID: blockchainIDB,
// 				DestinationAddress: fundedAddress,
// 				FeeInfo: FeeInfo{
// 					ContractAddress: fundedAddress,
// 					Amount:          big.NewInt(0),
// 				},
// 				RequiredGasLimit:        big.NewInt(1),
// 				AllowedRelayerAddresses: []common.Address{},
// 				Message:                 []byte{1, 2, 3, 4},
// 			},
// 		)
// 		Expect(err).Should(BeNil())

// 		// Send a transaction to the Teleporter contract
// 		tx := newTestTeleporterMessage(chainAIDInt, teleporterContractAddress, nonceA, data)

// 		txSigner := types.LatestSignerForChainID(chainAIDInt)
// 		signedTx, err := types.SignTx(tx, txSigner, fundedKey)
// 		Expect(err).Should(BeNil())

// 		// Sleep for some time to make sure relayer has started up and subscribed.
// 		time.Sleep(15 * time.Second)
// 		log.Info("Subscribing to new heads on destination chain")

// 		newHeadsB := make(chan *types.Header, 10)
// 		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
// 		Expect(err).Should(BeNil())
// 		defer sub.Unsubscribe()

// 		log.Info("Sending sendWarpMessage transaction", "destinationChainID", blockchainIDB, "txHash", signedTx.Hash())
// 		err = chainARPCClient.SendTransaction(ctx, signedTx)
// 		Expect(err).Should(BeNil())

// 		time.Sleep(5 * time.Second)
// 		receipt, err := chainARPCClient.TransactionReceipt(ctx, signedTx.Hash())
// 		Expect(err).Should(BeNil())
// 		Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

// 		// Get the latest block from Subnet B
// 		log.Info("Waiting for new block confirmation")
// 		newHead := <-newHeadsB
// 		log.Info("Received new head", "height", newHead.Number.Uint64())
// 		blockHash := newHead.Hash()
// 		block, err := chainBRPCClient.BlockByHash(ctx, blockHash)
// 		Expect(err).Should(BeNil())
// 		log.Info(
// 			"Got block",
// 			"blockHash", blockHash,
// 			"blockNumber", block.NumberU64(),
// 			"transactions", block.Transactions(),
// 			"numTransactions", len(block.Transactions()),
// 			"block", block,
// 		)
// 		accessLists := block.Transactions()[0].AccessList()
// 		Expect(len(accessLists)).Should(Equal(1))
// 		Expect(accessLists[0].Address).Should(Equal(warp.Module.Address))

// 		// Check the transaction storage key has warp message we're expecting
// 		storageKeyHashes := accessLists[0].StorageKeys
// 		packedPredicate := predicateutils.HashSliceToBytes(storageKeyHashes)
// 		predicateBytes, err := predicateutils.UnpackPredicate(packedPredicate)
// 		Expect(err).Should(BeNil())
// 		receivedWarpMessage, err = avalancheWarp.ParseMessage(predicateBytes)
// 		Expect(err).Should(BeNil())

// 		// Check that the transaction has successful receipt status
// 		txHash := block.Transactions()[0].Hash()
// 		receipt, err = chainBRPCClient.TransactionReceipt(ctx, txHash)
// 		Expect(err).Should(BeNil())
// 		Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

// 		log.Info("Finished sending warp message, closing down output channel")

// 		// Cancel the command and stop the relayer
// 		relayerCancel()
// 		_ = relayerCmd.Wait()
// 	})

// 	ginkgo.It("Try relaying already delivered message", ginkgo.Label("Relayer", "RelayerAlreadyDeliveredMessage"), func() {
// 		ctx := context.Background()
// 		logger := logging.NewLogger(
// 			"awm-relayer",
// 			logging.NewWrappedCore(
// 				logging.Info,
// 				os.Stdout,
// 				logging.JSON.ConsoleEncoder(),
// 			),
// 		)
// 		jsonDB, err := database.NewJSONFileStorage(logger, storageLocation, []ids.ID{blockchainIDA, blockchainIDB})
// 		Expect(err).Should(BeNil())

// 		// Modify the JSON database to force the relayer to re-process old blocks
// 		jsonDB.Put(blockchainIDA, []byte(database.LatestProcessedBlockKey), []byte("0"))
// 		jsonDB.Put(blockchainIDB, []byte(database.LatestProcessedBlockKey), []byte("0"))

// 		// Subscribe to the destination chain block published
// 		newHeadsB := make(chan *types.Header, 10)
// 		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
// 		Expect(err).Should(BeNil())
// 		defer sub.Unsubscribe()

// 		// Run the relayer
// 		relayerCmd, relayerCancel = runRelayerExecutable(ctx)

// 		// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
// 		Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

// 		// Cancel the command and stop the relayer
// 		relayerCancel()
// 		_ = relayerCmd.Wait()
// 	})

// 	ginkgo.It("Validate Received Warp Message Values", ginkgo.Label("Relayer", "VerifyWarp"), func() {
// 		Expect(receivedWarpMessage.SourceChainID).Should(Equal(blockchainIDA))
// 		addressedPayload, err := warpPayload.ParseAddressedPayload(receivedWarpMessage.Payload)
// 		Expect(err).Should(BeNil())

// 		receivedDestinationID, err := ids.ToID(addressedPayload.DestinationChainID.Bytes())
// 		Expect(err).Should(BeNil())
// 		Expect(receivedDestinationID).Should(Equal(blockchainIDB))
// 		Expect(addressedPayload.DestinationAddress).Should(Equal(teleporterContractAddress))
// 		Expect(addressedPayload.Payload).Should(Equal(payload))

// 		// Check that the teleporter message is correct
// 		receivedTeleporterMessage, err := teleporter.UnpackTeleporterMessage(addressedPayload.Payload)
// 		Expect(err).Should(BeNil())
// 		Expect(*receivedTeleporterMessage).Should(Equal(teleporterMessage))
// 	})
// })

var _ = ginkgo.Describe("[Relayer Publish Block Hash]", ginkgo.Ordered, func() {
	var (
		relayerCmd                *exec.Cmd
		relayerCancel             context.CancelFunc
		blockHashReceiverAddressB common.Address
		subnetAHashes             []common.Hash
		blockHashABI              *abi.ABI
	)
	ginkgo.It("Deploy block hash receiver", ginkgo.Label("Relayer", "DeployBlockHashReceiver"), func() {
		ctx := context.Background()
		blockHashReceiverByteCode := readHexTextFile("./tests/BlockHashReceiverByteCode.txt")

		nonceB, err := chainBRPCClient.NonceAt(ctx, fundedAddress, nil)
		Expect(err).Should(BeNil())

		// gasTipCapA, err := chainARPCClient.SuggestGasTipCap(context.Background())
		// Expect(err).Should(BeNil())

		// baseFeeA, err := chainARPCClient.EstimateBaseFee(context.Background())
		// Expect(err).Should(BeNil())
		// gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
		// gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

		// contractAuth, err := bind.NewKeyedTransactorWithChainID(fundedKey, big.NewInt(peers.LocalNetworkID))
		// contractAuth.Nonce = big.NewInt(int64(nonceA))
		// contractAuth.GasFeeCap = gasFeeCapA
		// contractAuth.GasTipCap = gasTipCapA
		// contractAuth.GasLimit = 1000000

		// Expect(err).Should(BeNil())
		// blockHashReceiverAddressB, _, _, err = bind.DeployContract(
		// 	contractAuth,
		// 	*blockHashABI,
		// 	common.FromHex(blockHashReceiverByteCode),
		// 	chainBRPCClient,
		// )
		// Expect(err).Should(BeNil())
		blockHashABI, err = teleporter_block_hash.TeleporterBlockHashMetaData.GetAbi()
		Expect(err).Should(BeNil())
		blockHashReceiverAddressB, err = deriveEVMContractAddress(fundedAddress, nonceB)
		Expect(err).Should(BeNil())

		cmdOutput := make(chan string)
		cmd := exec.Command(
			"cast",
			"send",
			"--rpc-url", chainBRPCURI,
			"--private-key", hexutil.Encode(fundedKey.D.Bytes()),
			"--create", blockHashReceiverByteCode,
		)

		// Set up a pipe to capture the command's output
		cmdReader, _ := cmd.StdoutPipe()

		// Start a goroutine to read and output the command's stdout
		go func() {
			scanner := bufio.NewScanner(cmdReader)
			for scanner.Scan() {
				log.Info(scanner.Text())
			}
			cmdOutput <- "Command execution finished"
		}()

		err = cmd.Run()
		Expect(err).Should(BeNil())

		time.Sleep(5 * time.Second)
		deployedCode, err := chainBRPCClient.CodeAt(ctx, blockHashReceiverAddressB, nil)
		Expect(err).Should(BeNil())
		Expect(len(deployedCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode

		log.Info("Deployed block hash receiver contract", "address", blockHashReceiverAddressB.Hex())
	})

	ginkgo.It("Set up relayer config", ginkgo.Label("Relayer", "SetupRelayer"), func() {
		hostA, portA, err := getURIHostAndPort(chainANodeURIs[0])
		Expect(err).Should(BeNil())

		hostB, portB, err := getURIHostAndPort(chainBNodeURIs[0])
		Expect(err).Should(BeNil())

		log.Info(
			"Setting up relayer config",
			"hostA", hostA,
			"portA", portA,
			"blockChainA", blockchainIDA.String(),
			"hostB", hostB,
			"portB", portB,
			"blockChainB", blockchainIDB.String(),
			"subnetA", subnetA.String(),
			"subnetB", subnetB.String(),
		)

		relayerConfig := config.Config{
			LogLevel:          logging.Info.LowerString(),
			NetworkID:         peers.LocalNetworkID,
			PChainAPIURL:      chainANodeURIs[0],
			EncryptConnection: false,
			StorageLocation:   storageLocation,
			SourceSubnets: []config.SourceSubnet{
				{
					SubnetID:          subnetA.String(),
					ChainID:           blockchainIDA.String(),
					VM:                config.EVM_BLOCKHASH.String(),
					EncryptConnection: false,
					APINodeHost:       hostA,
					APINodePort:       portA,
					MessageContracts: map[string]config.MessageProtocolConfig{
						"0x0000000000000000000000000000000000000000": {
							MessageFormat: config.BLOCK_HASH_PUBLISHER.String(),
							Settings: map[string]interface{}{
								"destination-chains": []struct {
									ChainID  string `json:"chain-id"`
									Address  string `json:"address"`
									Interval string `json:"interval"`
								}{
									{
										ChainID:  blockchainIDB.String(),
										Address:  blockHashReceiverAddressB.String(),
										Interval: "5",
									},
								},
							},
						},
					},
				},
			},
			DestinationSubnets: []config.DestinationSubnet{
				{
					SubnetID:          subnetB.String(),
					ChainID:           blockchainIDB.String(),
					VM:                config.EVM.String(),
					EncryptConnection: false,
					APINodeHost:       hostB,
					APINodePort:       portB,
					AccountPrivateKey: fundedKeyStr,
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

	ginkgo.It("Build Relayer", ginkgo.Label("Relayer", "BuildRelayer"), func() {
		// Build the awm-relayer binary
		cmd := exec.Command("./scripts/build.sh")
		out, err := cmd.CombinedOutput()
		fmt.Println(string(out))
		Expect(err).Should(BeNil())
	})

	// Send a transaction to Subnet A to issue a Warp Message from the Teleporter contract to Subnet B
	ginkgo.It("Publish block hash", ginkgo.Label("Relayer", "PublishHash"), func() {
		ctx := context.Background()

		relayerCmd, relayerCancel = runRelayerExecutable(ctx)
		nonceA, err := chainARPCClient.NonceAt(ctx, fundedAddress, nil)
		Expect(err).Should(BeNil())

		destinationAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
		gasTipCapA, err := chainARPCClient.SuggestGasTipCap(context.Background())
		Expect(err).Should(BeNil())

		baseFeeA, err := chainARPCClient.EstimateBaseFee(context.Background())
		Expect(err).Should(BeNil())
		gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
		gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

		// Subscribe to the destination chain block published
		newHeadsA := make(chan *types.Header, 10)
		subA, err := chainAWSClient.SubscribeNewHead(ctx, newHeadsA)
		Expect(err).Should(BeNil())
		defer subA.Unsubscribe()

		// Subscribe to the destination chain block published
		newHeadsB := make(chan *types.Header, 10)
		subB, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		Expect(err).Should(BeNil())
		defer subB.Unsubscribe()

		// TODONOW: not necessarily true, since the block height might be < 5
		// Send 5 transactions to produce 5 blocks on subnet A
		// We expect at exactly one of the block hashes to be published by the relayer
		for i := 0; i < 5; i++ {
			value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(1)) // 1eth
			txA := types.NewTx(&types.DynamicFeeTx{
				ChainID:   chainAIDInt,
				Nonce:     nonceA + uint64(i),
				To:        &destinationAddress,
				Gas:       defaultTeleporterMessageGas,
				GasFeeCap: gasFeeCapA,
				GasTipCap: gasTipCapA,
				Value:     value,
			})
			txSignerA := types.LatestSignerForChainID(chainAIDInt)
			triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
			Expect(err).Should(BeNil())
			err = chainARPCClient.SendTransaction(ctx, triggerTxA)
			Expect(err).Should(BeNil())

			log.Info("Waiting for new block confirmation", "block", i)
			newHeadA := <-newHeadsA
			subnetAHashes = append(subnetAHashes, newHeadA.Hash())
		}

		time.Sleep(5 * time.Second)

		for {
			newHeadB := <-newHeadsB
			log.Info("Fetching log from the newly produced block")

			blockHashB := newHeadB.Hash()

			block, err := chainBRPCClient.BlockByHash(ctx, blockHashB)
			Expect(err).Should(BeNil())
			txs := block.Transactions()
			log.Info(fmt.Sprintf("numTxs: %d", len(txs)))
			for _, tx := range txs {
				log.Info(fmt.Sprintf("txHash: %s", tx.Hash().String()))
				log.Info(fmt.Sprintf("to: %s", tx.To().String()))
				log.Info(fmt.Sprintf("data: %s", hex.EncodeToString(tx.Data())))
				receipt, err := chainBRPCClient.TransactionReceipt(ctx, tx.Hash())
				Expect(err).Should(BeNil())
				Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))
			}

			// receipt, err := chainBRPCClient.TransactionReceipt(ctx, blockHashB)
			// Expect(err).Should(BeNil())
			// Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

			logs, err := chainBRPCClient.FilterLogs(ctx, interfaces.FilterQuery{
				BlockHash: &blockHashB,
				Addresses: []common.Address{blockHashReceiverAddressB},
				Topics: [][]common.Hash{
					{
						blockHashABI.Events["ReceiveBlockHash"].ID,
					},
				},
			})
			Expect(err).Should(BeNil())
			log.Info("Logs", "logs", logs)
		}
		// // Wait for new blocks on B. We are expecting 2 new blocks
		// {
		// 	newHeadB := <-newHeadsB
		// 	blockHashB := newHeadB.Hash()

		// 	log.Info("Fetching relevant warp logs from the newly produced block")
		// 	logs, err := chainBRPCClient.FilterLogs(ctx, interfaces.FilterQuery{
		// 		BlockHash: &blockHashB,
		// 		// Addresses: []common.Address{blockHashReceiverAddressB},
		// 		// Topics: [][]common.Hash{
		// 		// 	{
		// 		// 		blockHashABI.Events["ReceiveBlockHash"].ID,
		// 		// 	},
		// 		// },
		// 	})
		// 	log.Info("Logs", "logs", logs)
		// 	Expect(err).Should(BeNil())
		// 	Expect(len(logs)).Should(Equal(1))

		// 	Expect(logs[0].Topics[2]).Should(Equal(subnetAHashes[0]))
		// }
		// {
		// 	newHeadB := <-newHeadsB
		// 	blockHashB := newHeadB.Hash()

		// 	log.Info("Fetching relevant warp logs from the newly produced block")
		// 	logs, err := chainBRPCClient.FilterLogs(ctx, interfaces.FilterQuery{
		// 		BlockHash: &blockHashB,
		// 		Addresses: []common.Address{blockHashReceiverAddressB},
		// 		Topics: [][]common.Hash{
		// 			{
		// 				blockHashABI.Events["ReceiveBlockHash"].ID,
		// 			},
		// 		},
		// 	})
		// 	Expect(err).Should(BeNil())
		// 	Expect(len(logs)).Should(Equal(1))

		// 	Expect(logs[0].Topics[2]).Should(Equal(subnetAHashes[5]))
		// }

		// Cancel the command and stop the relayer
		relayerCancel()
		_ = relayerCmd.Wait()
	})

	ginkgo.It("Verify received block hash", ginkgo.Label("Relayer", "VerifyReceivedHash"), func() {

	})
})
