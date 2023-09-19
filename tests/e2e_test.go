// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"crypto/ecdsa"
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
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/awm-relayer/peers"
	relayerEvm "github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/tests/utils"
	"github.com/ava-labs/subnet-evm/tests/utils/runner"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
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
	storageLocation = fmt.Sprintf("%s/.awm-relayer-storage", os.TempDir())
)

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	gomega.RegisterFailHandler(ginkgo.Fail)
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
	gomega.Expect(err).Should(gomega.BeNil())
	_, err = f.Write([]byte(`{"warp-api-enabled": true}`))
	gomega.Expect(err).Should(gomega.BeNil())
	warpChainConfigPath = f.Name()

	// Make sure that the warp genesis file exists
	_, err = os.Stat(warpGenesisFile)
	gomega.Expect(err).Should(gomega.BeNil())

	// Construct the network using the avalanche-network-runner
	_, err = manager.StartDefaultNetwork(ctx)
	gomega.Expect(err).Should(gomega.BeNil())
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
	gomega.Expect(err).Should(gomega.BeNil())

	// Issue transactions to activate the proposerVM fork on the receiving chain
	fundedKey, err := crypto.HexToECDSA(fundedKeyStr)
	gomega.Expect(err).Should(gomega.BeNil())
	subnetB := manager.GetSubnets()[1]
	subnetBDetails, ok := manager.GetSubnet(subnetB)
	gomega.Expect(ok).Should(gomega.BeTrue())

	chainBID := subnetBDetails.BlockchainID
	uri := httpToWebsocketURI(subnetBDetails.ValidatorURIs[0], chainBID.String())

	client, err := ethclient.Dial(uri)
	gomega.Expect(err).Should(gomega.BeNil())
	chainBIDInt, err := client.ChainID(ctx)
	gomega.Expect(err).Should(gomega.BeNil())

	err = utils.IssueTxsToActivateProposerVMFork(ctx, chainBIDInt, fundedKey, client)
	gomega.Expect(err).Should(gomega.BeNil())

	log.Info("Set up ginkgo before suite")
})

var _ = ginkgo.AfterSuite(func() {
	log.Info("Running ginkgo after suite")
	gomega.Expect(manager).ShouldNot(gomega.BeNil())
	gomega.Expect(manager.TeardownNetwork()).Should(gomega.BeNil())
	gomega.Expect(os.Remove(warpChainConfigPath)).Should(gomega.BeNil())
	gomega.Expect(os.Remove(relayerConfigPath)).Should(gomega.BeNil())
})

// Ginkgo describe node that acts as a container for the relayer e2e tests. This test suite
// will run in order, starting off by setting up the subnet URIs and creating a relayer config
// file. It will then build the relayer binary and run it with the config file. The relayer
// will then send a transaction to the source subnet to issue a Warp message simulting a transaction
// sent from the Teleporter contract. The relayer will then wait for the transaction to be confirmed
// on the destination subnet and verify that the Warp message was received and unpacked correctly.
var _ = ginkgo.Describe("[Relayer E2E]", ginkgo.Ordered, func() {
	var (
		subnetIDs                        []ids.ID
		subnetA, subnetB                 ids.ID
		blockchainIDA, blockchainIDB     ids.ID
		chainANodeURIs, chainBNodeURIs   []string
		fundedKey                        *ecdsa.PrivateKey
		err                              error
		receivedWarpMessage              *avalancheWarp.Message
		chainBWSClient                   ethclient.Client
		chainARPCClient, chainBRPCClient ethclient.Client
		chainARPCURI, chainBRPCURI       string
		chainAIDInt                      *big.Int
		chainBIDInt                      *big.Int
		payload                          []byte
		relayerCmd                       *exec.Cmd
		relayerCancel                    context.CancelFunc
	)

	fundedKey, err = crypto.HexToECDSA(fundedKeyStr)
	if err != nil {
		panic(err)
	}

	ginkgo.It("Setup subnet URIs", ginkgo.Label("Relayer", "Setup"), func() {
		subnetIDs = manager.GetSubnets()
		gomega.Expect(len(subnetIDs)).Should(gomega.Equal(2))

		subnetA = subnetIDs[0]
		subnetADetails, ok := manager.GetSubnet(subnetA)
		gomega.Expect(ok).Should(gomega.BeTrue())
		gomega.Expect(len(subnetADetails.ValidatorURIs)).Should(gomega.Equal(5))
		blockchainIDA = subnetADetails.BlockchainID
		chainANodeURIs = append(chainANodeURIs, subnetADetails.ValidatorURIs...)

		subnetB = subnetIDs[1]
		subnetBDetails, ok := manager.GetSubnet(subnetB)
		gomega.Expect(ok).Should(gomega.BeTrue())
		gomega.Expect(len(subnetBDetails.ValidatorURIs)).Should(gomega.Equal(5))
		blockchainIDB = subnetBDetails.BlockchainID
		chainBNodeURIs = append(chainBNodeURIs, subnetBDetails.ValidatorURIs...)

		log.Info("Created URIs for both subnets", "ChainAURIs", chainANodeURIs, "ChainBURIs", chainBNodeURIs, "blockchainIDA", blockchainIDA, "blockchainIDB", blockchainIDB)

		chainAWSURI := httpToWebsocketURI(chainANodeURIs[0], blockchainIDA.String())
		chainARPCURI = httpToRPCURI(chainANodeURIs[0], blockchainIDA.String())
		log.Info("Creating ethclient for blockchainA", "wsURI", chainAWSURI, "rpcURL, chainARPCURI")
		chainARPCClient, err = ethclient.Dial(chainARPCURI)
		gomega.Expect(err).Should(gomega.BeNil())

		chainAIDInt, err = chainARPCClient.ChainID(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())

		chainBWSURI := httpToWebsocketURI(chainBNodeURIs[0], blockchainIDB.String())
		chainBRPCURI = httpToRPCURI(chainBNodeURIs[0], blockchainIDB.String())
		log.Info("Creating ethclient for blockchainB", "wsURI", chainBWSURI)
		chainBWSClient, err = ethclient.Dial(chainBWSURI)
		gomega.Expect(err).Should(gomega.BeNil())
		chainBRPCClient, err = ethclient.Dial(chainBRPCURI)
		gomega.Expect(err).Should(gomega.BeNil())

		chainBIDInt, err = chainBRPCClient.ChainID(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Finished setting up e2e test subnet variables")
	})

	ginkgo.It("Deploy Teleporter Contract", ginkgo.Label("Relayer", "Deploy Teleporter"), func() {
		ctx := context.Background()

		// Read in the Teleporter contract information
		teleporterContractAddress = common.BytesToAddress(readHexTextFile("./tests/UniversalTeleporterMessengerContractAddress.txt"))
		teleporterDeployerAddress := common.BytesToAddress(readHexTextFile("./tests/UniversalTeleporterDeployerAddress.txt"))
		teleporterDeployerTransaction := readHexTextFile("./tests/UniversalTeleporterDeployerTransaction.txt")

		nonceA, err := chainARPCClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		nonceB, err := chainBRPCClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		gasTipCapA, err := chainARPCClient.SuggestGasTipCap(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())
		gasTipCapB, err := chainBRPCClient.SuggestGasTipCap(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())

		baseFeeA, err := chainARPCClient.EstimateBaseFee(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())
		gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
		gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

		baseFeeB, err := chainBRPCClient.EstimateBaseFee(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())
		gasFeeCapB := baseFeeB.Mul(baseFeeB, big.NewInt(relayerEvm.BaseFeeFactor))
		gasFeeCapB.Add(gasFeeCapB, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

		// Fund the deployer address
		{
			value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth
			txA := types.NewTx(&types.DynamicFeeTx{
				ChainID:   chainAIDInt,
				Nonce:     nonceA,
				To:        &teleporterDeployerAddress,
				Gas:       defaultTeleporterMessageGas,
				GasFeeCap: gasFeeCapA,
				GasTipCap: gasTipCapA,
				Value:     value,
			})
			txSignerA := types.LatestSignerForChainID(chainAIDInt)
			triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
			gomega.Expect(err).Should(gomega.BeNil())
			err = chainARPCClient.SendTransaction(ctx, triggerTxA)
			gomega.Expect(err).Should(gomega.BeNil())
			time.Sleep(5 * time.Second)
			receipt, err := chainARPCClient.TransactionReceipt(ctx, triggerTxA.Hash())
			gomega.Expect(err).Should(gomega.BeNil())
			gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))
		}
		{
			value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth
			txB := types.NewTx(&types.DynamicFeeTx{
				ChainID:   chainBIDInt,
				Nonce:     nonceB,
				To:        &teleporterDeployerAddress,
				Gas:       defaultTeleporterMessageGas,
				GasFeeCap: gasFeeCapB,
				GasTipCap: gasTipCapB,
				Value:     value,
			})
			txSignerB := types.LatestSignerForChainID(chainBIDInt)
			triggerTxB, err := types.SignTx(txB, txSignerB, fundedKey)
			gomega.Expect(err).Should(gomega.BeNil())
			err = chainBRPCClient.SendTransaction(ctx, triggerTxB)
			gomega.Expect(err).Should(gomega.BeNil())
			time.Sleep(5 * time.Second)
			receipt, err := chainBRPCClient.TransactionReceipt(ctx, triggerTxB.Hash())
			gomega.Expect(err).Should(gomega.BeNil())
			gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))
		}
		// Deploy Teleporter on the two subnets
		{
			rpcClient, err := rpc.DialContext(ctx, chainARPCURI)
			gomega.Expect(err).Should(gomega.BeNil())
			err = rpcClient.CallContext(ctx, nil, "eth_sendRawTransaction", hexutil.Encode(teleporterDeployerTransaction))
			gomega.Expect(err).Should(gomega.BeNil())
			time.Sleep(5 * time.Second)
			teleporterCode, err := chainARPCClient.CodeAt(ctx, teleporterContractAddress, nil)
			gomega.Expect(err).Should(gomega.BeNil())
			gomega.Expect(len(teleporterCode)).Should(gomega.BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode
		}
		{
			rpcClient, err := rpc.DialContext(ctx, chainBRPCURI)
			gomega.Expect(err).Should(gomega.BeNil())
			err = rpcClient.CallContext(ctx, nil, "eth_sendRawTransaction", hexutil.Encode(teleporterDeployerTransaction))
			gomega.Expect(err).Should(gomega.BeNil())
			time.Sleep(5 * time.Second)
			teleporterCode, err := chainBRPCClient.CodeAt(ctx, teleporterContractAddress, nil)
			gomega.Expect(err).Should(gomega.BeNil())
			gomega.Expect(len(teleporterCode)).Should(gomega.BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode
		}
	})

	ginkgo.It("Set up relayer config", ginkgo.Label("Relayer", "Setup Relayer"), func() {
		hostA, portA, err := getURIHostAndPort(chainANodeURIs[0])
		gomega.Expect(err).Should(gomega.BeNil())

		hostB, portB, err := getURIHostAndPort(chainBNodeURIs[0])
		gomega.Expect(err).Should(gomega.BeNil())

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
		gomega.Expect(err).Should(gomega.BeNil())

		f, err := os.CreateTemp(os.TempDir(), "relayer-config.json")
		gomega.Expect(err).Should(gomega.BeNil())

		_, err = f.Write(data)
		gomega.Expect(err).Should(gomega.BeNil())
		relayerConfigPath = f.Name()

		log.Info("Created awm-relayer config", "configPath", relayerConfigPath, "config", string(data))
	})

	ginkgo.It("Build Relayer", ginkgo.Label("Relayer", "Build Relayer"), func() {
		// Build the awm-relayer binary
		cmd := exec.Command("./scripts/build.sh")
		out, err := cmd.CombinedOutput()
		fmt.Println(string(out))
		gomega.Expect(err).Should(gomega.BeNil())
	})

	// Send a transaction to Subnet A to issue a Warp Message from the Teleporter contract to Subnet B
	ginkgo.It("Send Message from A to B", ginkgo.Label("Warp", "SendWarp"), func() {
		ctx := context.Background()

		relayerCmd, relayerCancel = runRelayerExecutable(ctx)

		nonceA, err := chainARPCClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		nonceB, err := chainBRPCClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Packing teleporter message", "nonceA", nonceA, "nonceB", nonceB)
		payload, err = teleporter.PackTeleporterMessage(common.Hash(blockchainIDB), teleporterMessage)
		gomega.Expect(err).Should(gomega.BeNil())

		data, err := teleporter.EVMTeleporterContractABI.Pack(
			"sendCrossChainMessage",
			TeleporterMessageInput{
				DestinationChainID: blockchainIDB,
				DestinationAddress: fundedAddress,
				FeeInfo: FeeInfo{
					ContractAddress: fundedAddress,
					Amount:          big.NewInt(0),
				},
				RequiredGasLimit:        big.NewInt(1),
				AllowedRelayerAddresses: []common.Address{},
				Message:                 []byte{1, 2, 3, 4},
			},
		)
		gomega.Expect(err).Should(gomega.BeNil())

		// Send a transaction to the Teleporter contract
		tx := newTestTeleporterMessage(chainAIDInt, teleporterContractAddress, nonceA, data)

		txSigner := types.LatestSignerForChainID(chainAIDInt)
		signedTx, err := types.SignTx(tx, txSigner, fundedKey)
		gomega.Expect(err).Should(gomega.BeNil())

		// Sleep for some time to make sure relayer has started up and subscribed.
		time.Sleep(15 * time.Second)
		log.Info("Subscribing to new heads on destination chain")

		newHeadsB := make(chan *types.Header, 10)
		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		gomega.Expect(err).Should(gomega.BeNil())
		defer sub.Unsubscribe()

		log.Info("Sending sendWarpMessage transaction", "destinationChainID", blockchainIDB, "txHash", signedTx.Hash())
		err = chainARPCClient.SendTransaction(ctx, signedTx)
		gomega.Expect(err).Should(gomega.BeNil())

		time.Sleep(5 * time.Second)
		receipt, err := chainARPCClient.TransactionReceipt(ctx, signedTx.Hash())
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))

		// Get the latest block from Subnet B
		log.Info("Waiting for new block confirmation")
		newHead := <-newHeadsB
		log.Info("Received new head", "height", newHead.Number.Uint64())
		blockHash := newHead.Hash()
		block, err := chainBRPCClient.BlockByHash(ctx, blockHash)
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info(
			"Got block",
			"blockHash", blockHash,
			"blockNumber", block.NumberU64(),
			"transactions", block.Transactions(),
			"numTransactions", len(block.Transactions()),
			"block", block,
		)
		accessLists := block.Transactions()[0].AccessList()
		gomega.Expect(len(accessLists)).Should(gomega.Equal(1))
		gomega.Expect(accessLists[0].Address).Should(gomega.Equal(warp.Module.Address))

		// Check the transaction storage key has warp message we're expecting
		storageKeyHashes := accessLists[0].StorageKeys
		packedPredicate := predicateutils.HashSliceToBytes(storageKeyHashes)
		predicateBytes, err := predicateutils.UnpackPredicate(packedPredicate)
		gomega.Expect(err).Should(gomega.BeNil())
		receivedWarpMessage, err = avalancheWarp.ParseMessage(predicateBytes)
		gomega.Expect(err).Should(gomega.BeNil())

		// Check that the transaction has successful receipt status
		txHash := block.Transactions()[0].Hash()
		receipt, err = chainBRPCClient.TransactionReceipt(ctx, txHash)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))

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
		jsonDB, err := database.NewJSONFileStorage(logger, storageLocation, []ids.ID{blockchainIDA, blockchainIDB})
		gomega.Expect(err).Should(gomega.BeNil())

		// Modify the JSON database to force the relayer to re-process old blocks
		jsonDB.Put(blockchainIDA, []byte(database.LatestProcessedBlockKey), []byte("0"))
		jsonDB.Put(blockchainIDB, []byte(database.LatestProcessedBlockKey), []byte("0"))

		// Subscribe to the destination chain block published
		newHeadsB := make(chan *types.Header, 10)
		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		gomega.Expect(err).Should(gomega.BeNil())
		defer sub.Unsubscribe()

		// Run the relayer
		relayerCmd, relayerCancel = runRelayerExecutable(ctx)

		// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
		gomega.Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(gomega.Receive())

		// Cancel the command and stop the relayer
		relayerCancel()
		_ = relayerCmd.Wait()
	})

	ginkgo.It("Validate Received Warp Message Values", ginkgo.Label("Relaery", "VerifyWarp"), func() {
		gomega.Expect(receivedWarpMessage.SourceChainID).Should(gomega.Equal(blockchainIDA))
		addressedPayload, err := warpPayload.ParseAddressedPayload(receivedWarpMessage.Payload)
		gomega.Expect(err).Should(gomega.BeNil())

		receivedDestinationID, err := ids.ToID(addressedPayload.DestinationChainID.Bytes())
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(receivedDestinationID).Should(gomega.Equal(blockchainIDB))
		gomega.Expect(addressedPayload.DestinationAddress).Should(gomega.Equal(teleporterContractAddress))
		gomega.Expect(addressedPayload.Payload).Should(gomega.Equal(payload))

		// Check that the teleporter message is correct
		receivedTeleporterMessage, err := teleporter.UnpackTeleporterMessage(addressedPayload.Payload)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(*receivedTeleporterMessage).Should(gomega.Equal(teleporterMessage))
	})
})
