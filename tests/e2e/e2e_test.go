// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/messages/teleporter"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/tests/utils"
	"github.com/ava-labs/subnet-evm/tests/utils/runner"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const fundedKeyStr = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
const teleporterKeyStr = "d26c3344074df322e7c66ad0d2357d96f0d07d0f40038264d1d64d276709da41"

var (
	anrConfig                 = runner.NewDefaultANRConfig()
	manager                   = runner.NewNetworkManager(anrConfig)
	warpChainConfigPath       string
	relayerConfigPath         string
	teleporterContractAddress = common.HexToAddress("1dD31B5351e76d51F4B152ce64fE5cf594694De5")
	teleporterMessage         = teleporter.TeleporterMessage{
		MessageID:               big.NewInt(1),
		SenderAddress:           common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		DestinationAddress:      common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		RequiredGasLimit:        big.NewInt(1),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleporter.TeleporterMessageReceipt{},
		Message:                 []byte{1, 2, 3, 4},
	}
)

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Relayer e2e test")
}

func toWebsocketURI(uri string, blockchainID string) string {
	return fmt.Sprintf("ws://%s/ext/bc/%s/ws", strings.TrimPrefix(uri, "http://"), blockchainID)
}

func toRPCURI(uri string, blockchainID string) string {
	return fmt.Sprintf("http://%s/ext/bc/%s/rpc", strings.TrimPrefix(uri, "http://"), blockchainID)
}

// BeforeSuite starts the default network and adds 10 new nodes as validators with BLS keys
// registered on the P-Chain.
// Adds two disjoint sets of 5 of the new validator nodes to validate two new subnets with a
// a single Subnet-EVM blockchain.
var _ = ginkgo.BeforeSuite(func() {
	ctx := context.Background()
	var err error

	// Name 10 new validators (which should have BLS key registered)
	subnetANodeNames := make([]string, 0)
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

	// Construct the network using the avalanche-network-runner
	_, err = manager.StartDefaultNetwork(ctx)
	gomega.Expect(err).Should(gomega.BeNil())
	err = manager.SetupNetwork(
		ctx,
		anrConfig.AvalancheGoExecPath,
		[]*rpcpb.BlockchainSpec{
			{
				VmName:      evm.IDStr,
				Genesis:     "./tests/e2e/warp.json",
				ChainConfig: warpChainConfigPath,
				SubnetSpec: &rpcpb.SubnetSpec{
					SubnetConfig: "",
					Participants: subnetANodeNames,
				},
			},
			{
				VmName:      evm.IDStr,
				Genesis:     "./tests/e2e/warp.json",
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
	uri := toWebsocketURI(subnetBDetails.ValidatorURIs[0], chainBID.String())
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

var _ = ginkgo.Describe("[Relayer]", ginkgo.Ordered, func() {
	var (
		subnetIDs                      []ids.ID
		subnetA, subnetB               ids.ID
		blockchainIDA, blockchainIDB   ids.ID
		chainAURIs, chainBURIs         []string
		fundedKey                      *ecdsa.PrivateKey
		fundedAddress                  common.Address
		teleporterKey                  *ecdsa.PrivateKey
		err                            error
		receivedWarpMessage            *avalancheWarp.Message
		chainAWSClient, chainBWSClient ethclient.Client
		chainAIDInt                    *big.Int
		payload                        = []byte{}
	)

	fundedKey, err = crypto.HexToECDSA(fundedKeyStr)
	if err != nil {
		panic(err)
	}
	fundedAddress = crypto.PubkeyToAddress(fundedKey.PublicKey)

	teleporterKey, err = crypto.HexToECDSA(teleporterKeyStr)
	if err != nil {
		panic(err)
	}

	ginkgo.It("Setup subnet URIs", ginkgo.Label("Relayer", "Setup"), func() {
		subnetIDs = manager.GetSubnets()
		gomega.Expect(len(subnetIDs)).Should(gomega.Equal(2))

		subnetA = subnetIDs[0]
		subnetADetails, ok := manager.GetSubnet(subnetA)
		gomega.Expect(ok).Should(gomega.BeTrue())
		blockchainIDA = subnetADetails.BlockchainID
		gomega.Expect(len(subnetADetails.ValidatorURIs)).Should(gomega.Equal(5))
		chainAURIs = append(chainAURIs, subnetADetails.ValidatorURIs...)

		subnetB = subnetIDs[1]
		subnetBDetails, ok := manager.GetSubnet(subnetB)
		gomega.Expect(ok).Should(gomega.BeTrue())
		blockchainIDB = subnetBDetails.BlockchainID
		gomega.Expect(len(subnetBDetails.ValidatorURIs)).Should(gomega.Equal(5))
		chainBURIs = append(chainBURIs, subnetBDetails.ValidatorURIs...)

		log.Info("Created URIs for both subnets", "ChainAURIs", chainAURIs, "ChainBURIs", chainBURIs, "blockchainIDA", blockchainIDA, "blockchainIDB", blockchainIDB)

		chainAWSURI := toWebsocketURI(chainAURIs[0], blockchainIDA.String())
		log.Info("Creating ethclient for blockchainA", "wsURI", chainAWSURI)
		chainAWSClient, err = ethclient.Dial(chainAWSURI)
		gomega.Expect(err).Should(gomega.BeNil())

		chainAIDInt, err = chainAWSClient.ChainID(context.Background())
		gomega.Expect(err).Should(gomega.BeNil())

		chainBWSURI := toWebsocketURI(chainBURIs[0], blockchainIDB.String())
		log.Info("Creating ethclient for blockchainB", "wsURI", chainBWSURI)
		chainBWSClient, err = ethclient.Dial(chainBWSURI)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Finished setting up e2e test subnet variables")
	})

	ginkgo.It("Set up relayer config", ginkgo.Label("Relayer", "Setup Relayer"), func() {
		hostA, portA, err := getURIHostAndPort(chainAURIs[0])
		gomega.Expect(err).Should(gomega.BeNil())

		hostB, portB, err := getURIHostAndPort(chainBURIs[0])
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Setting up relayer config", "hostA", hostA, "portA", portA, "blockChainA", blockchainIDA.String(), "hostB", hostB, "portB", portB, "blockChainB", blockchainIDB.String(), "subnetA", subnetA.String(), "subnetB", subnetB.String())

		relayerConfig := config.Config{
			LogLevel:          logging.Info.LowerString(),
			NetworkID:         1337,
			PChainAPIURL:      chainAURIs[0],
			EncryptConnection: false,
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
							MessageFormat: "teleporter",
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

		// relayerConfigPath := "./tests/e2e/relayer-config.json"
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

	// Send a transaction to Subnet A to issue a Warp Message to Subnet B
	ginkgo.It("Send Message from A to B", ginkgo.Label("Warp", "SendWarp"), func() {
		ctx := context.Background()

		// Create a channel to communicate with the goroutine
		cmdOutput := make(chan string)

		// Run awm relayer binary with config path
		relayerContext, relayerCancel := context.WithCancel(ctx)
		cmd := exec.CommandContext(relayerContext, "./build/awm-relayer", "--config-file", relayerConfigPath)

		// Set up a pipe to capture the command's output
		cmdReader, _ := cmd.StdoutPipe()

		// Start the command
		err := cmd.Start()
		gomega.Expect(err).Should(gomega.BeNil())

		// Start a goroutine to read and output the command's stdout
		go func() {
			scanner := bufio.NewScanner(cmdReader)
			for scanner.Scan() {
				log.Info(scanner.Text())
			}
			cmdOutput <- "Command execution finished"
		}()

		gomega.Expect(err).Should(gomega.BeNil())

		time.Sleep(15 * time.Second)
		log.Info("Subscribing to new heads")

		newHeadsB := make(chan *types.Header, 10)
		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeadsB)
		gomega.Expect(err).Should(gomega.BeNil())
		defer sub.Unsubscribe()

		startingNonce, err := chainAWSClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		nonce, err := chainBWSClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Packing teleporter message", "nonceA", startingNonce, "nonceB", nonce)

		payload, err = teleporter.PackTeleporterMessage(common.Hash(blockchainIDB), teleporterMessage)
		gomega.Expect(err).Should(gomega.BeNil())

		packedInput, err := warp.PackSendWarpMessage(warp.SendWarpMessageInput{
			DestinationChainID: common.Hash(blockchainIDB),
			DestinationAddress: teleporterContractAddress,
			Payload:            payload,
		})
		gomega.Expect(err).Should(gomega.BeNil())
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainAIDInt,
			Nonce:     startingNonce,
			To:        &warp.Module.Address,
			Gas:       200_000,
			GasFeeCap: big.NewInt(225 * params.GWei),
			GasTipCap: big.NewInt(params.GWei),
			Value:     common.Big0,
			Data:      packedInput,
		})
		txSigner := types.LatestSignerForChainID(chainAIDInt)
		signedTx, err := types.SignTx(tx, txSigner, teleporterKey)
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info("Sending sendWarpMessage transaction", "destinationChainID", blockchainIDB, "txHash", signedTx.Hash())
		err = chainAWSClient.SendTransaction(ctx, signedTx)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Waiting for new block confirmation")
		var blockHash common.Hash
		newHead := <-newHeadsB
		log.Info("Received new head", "height", newHead.Number.Uint64())
		blockHash = newHead.Hash()

		block, err := chainBWSClient.BlockByHash(ctx, blockHash)
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info("Got block", "blockHash", blockHash, "blockNumber", block.NumberU64(), "transactions", block.Transactions(), "block", block)
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

		// Check that the transaction is no longer pending
		txHash := block.Transactions()[0].Hash()
		_, isPending, err := chainBWSClient.TransactionByHash(ctx, txHash)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(isPending).Should(gomega.BeFalse())

		receipt, err := chainBWSClient.TransactionReceipt(ctx, txHash)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))

		log.Info("Finished sending warp message, closing down output channel")

		// Cancel the command and stop the relayer
		relayerCancel()
		_ = cmd.Wait()
	})

	ginkgo.It("Verify Warp Message", ginkgo.Label("Relay", "VerifyWarp"), func() {
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

func getURIHostAndPort(uri string) (string, uint32, error) {
	// At a minimum uri should have http:// of 7 characters
	gomega.Expect(len(uri)).Should(gomega.BeNumerically(">", 7))
	if uri[:7] == "http://" {
		uri = uri[7:]
	} else if uri[:8] == "https://" {
		uri = uri[8:]
	} else {
		return "", 0, fmt.Errorf("invalid uri: %s", uri)
	}

	// Split the uri into host and port
	hostAndPort := strings.Split(uri, ":")
	gomega.Expect(len(hostAndPort)).Should(gomega.Equal(2))

	// Parse the port
	port, err := strconv.ParseUint(hostAndPort[1], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse port: %w", err)
	}

	return hostAndPort[0], uint32(port), nil
}
