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
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	relayerEvm "github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/interfaces"
	teleporter_block_hash "github.com/ava-labs/teleporter/abi-bindings/Teleporter/TeleporterBlockHashReceiver"
	deploymentUtils "github.com/ava-labs/teleporter/contract-deployment/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"

	. "github.com/onsi/gomega"
)

func deployBlockHashReceiver(
	ctx context.Context,
	subnetInfo teleporterTestUtils.SubnetTestInfo,
	fundedAddress common.Address,
	fundedKey *ecdsa.PrivateKey,
	blockHashABI *abi.ABI,
	blockHashReceiverByteCode string,
) common.Address {
	nonce, err := subnetInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	blockHashReceiverAddress, err := deploymentUtils.DeriveEVMContractAddress(fundedAddress, nonce)
	Expect(err).Should(BeNil())

	cmdOutput := make(chan string)
	cmd := exec.Command(
		"cast",
		"send",
		"--rpc-url", teleporterTestUtils.HttpToRPCURI(subnetInfo.ChainNodeURIs[0], subnetInfo.BlockchainID.String()),
		"--private-key", hexutil.Encode(fundedKey.D.Bytes()),
		"--create", blockHashReceiverByteCode,
	)

	// Set up a pipe to capture the command's output
	cmdReader, err := cmd.StdoutPipe()
	Expect(err).Should(BeNil())
	cmdStdErrReader, err := cmd.StderrPipe()
	Expect(err).Should(BeNil())

	// Start a goroutine to read and output the command's stdout
	go func() {
		scanner := bufio.NewScanner(cmdReader)
		for scanner.Scan() {
			log.Info(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()
	go func() {
		scanner := bufio.NewScanner(cmdStdErrReader)
		for scanner.Scan() {
			log.Error(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()

	err = cmd.Run()
	Expect(err).Should(BeNil())

	// Confirm successful deployment
	deployedCode, err := subnetInfo.ChainWSClient.CodeAt(ctx, blockHashReceiverAddress, nil)
	Expect(err).Should(BeNil())
	Expect(len(deployedCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode

	log.Info("Deployed block hash receiver contract", "address", blockHashReceiverAddress.Hex())

	return blockHashReceiverAddress
}

func receiveBlockHash(
	ctx context.Context,
	blockHashReceiverAddress common.Address,
	newHeads chan *types.Header,
	subnetInfo teleporterTestUtils.SubnetTestInfo,
	blockHashABI *abi.ABI, expectedHashes []common.Hash) {
	newHead := <-newHeads
	log.Info("Fetching log from the newly produced block")

	blockHashB := newHead.Hash()

	logs, err := subnetInfo.ChainWSClient.FilterLogs(ctx, interfaces.FilterQuery{
		BlockHash: &blockHashB,
		Addresses: []common.Address{blockHashReceiverAddress},
		Topics: [][]common.Hash{
			{
				blockHashABI.Events["ReceiveBlockHash"].ID,
			},
		},
	})
	Expect(err).Should(BeNil())

	bind, err := teleporter_block_hash.NewTeleporterBlockHashReceiver(blockHashReceiverAddress, subnetInfo.ChainWSClient)
	Expect(err).Should(BeNil())
	event, err := bind.ParseReceiveBlockHash(logs[0])
	Expect(err).Should(BeNil())

	// The published block hash should match one of the ones sent on Subnet A
	foundHash := false
	for _, blockHash := range expectedHashes {
		if hex.EncodeToString(blockHash[:]) == hex.EncodeToString(event.BlockHash[:]) {
			foundHash = true
			break
		}
	}
	if !foundHash {
		Expect(false).Should(BeTrue(), "published block hash does not match any of the sent block hashes")
	}
	log.Info(
		"Received published block hash on destination",
		"blockHash", hex.EncodeToString(event.BlockHash[:]),
		"destinationChainID", subnetInfo.BlockchainID.String(),
	)

	// We shouldn't receive any more blocks, since the relayer is configured to publish once every 5 blocks on the source
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeads, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())
}

func PublishBlockHashes() {
	subnetAInfo := teleporterTestUtils.GetSubnetATestInfo()
	subnetBInfo := teleporterTestUtils.GetSubnetBTestInfo()
	subnetCInfo := teleporterTestUtils.GetSubnetCTestInfo()
	fundedAddress, fundedKey := teleporterTestUtils.GetFundedAccountInfo()

	//
	// Deploy block hash receiver on Subnet B
	//
	ctx := context.Background()

	blockHashABI, err := teleporter_block_hash.TeleporterBlockHashReceiverMetaData.GetAbi()
	Expect(err).Should(BeNil())
	blockHashReceiverByteCode := testUtils.ReadHexTextFile("./tests/utils/BlockHashReceiverByteCode.txt")

	blockHashReceiverAddressB := deployBlockHashReceiver(ctx, subnetBInfo, fundedAddress, fundedKey, blockHashABI, blockHashReceiverByteCode)
	blockHashReceiverAddressC := deployBlockHashReceiver(ctx, subnetCInfo, fundedAddress, fundedKey, blockHashABI, blockHashReceiverByteCode)

	//
	// Setup relayer config
	//
	hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(subnetAInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(subnetBInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	hostC, portC, err := teleporterTestUtils.GetURIHostAndPort(subnetCInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	log.Info(
		"Setting up relayer config",
		"hostA", hostA,
		"portA", portA,
		"blockChainA", subnetAInfo.BlockchainID.String(),
		"subnetA", subnetAInfo.SubnetID.String(),
		"hostB", hostB,
		"portB", portB,
		"blockChainB", subnetBInfo.BlockchainID.String(),
		"subnetB", subnetBInfo.SubnetID.String(),
		"hostC", hostC,
		"portC", portC,
		"blockChainC", subnetCInfo.BlockchainID.String(),
		"subnetC", subnetCInfo.SubnetID.String(),
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
									ChainID:  subnetBInfo.BlockchainID.String(),
									Address:  blockHashReceiverAddressB.String(),
									Interval: "5",
								},
								{
									ChainID:  subnetCInfo.BlockchainID.String(),
									Address:  blockHashReceiverAddressC.String(),
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
				SubnetID:          subnetBInfo.SubnetID.String(),
				ChainID:           subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				AccountPrivateKey: hex.EncodeToString(fundedKey.D.Bytes()),
			},
			{
				SubnetID:          subnetCInfo.SubnetID.String(),
				ChainID:           subnetCInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostC,
				APINodePort:       portC,
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
	cmd := exec.Command("./scripts/build.sh")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	Expect(err).Should(BeNil())

	//
	// Publish block hashes
	//
	relayerCmd, relayerCancel := testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	destinationAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	gasTipCapA, err := subnetAInfo.ChainWSClient.SuggestGasTipCap(context.Background())
	Expect(err).Should(BeNil())

	baseFeeA, err := subnetAInfo.ChainWSClient.EstimateBaseFee(context.Background())
	Expect(err).Should(BeNil())
	gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
	gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

	// Subscribe to the destination chain block published
	newHeadsB := make(chan *types.Header, 10)
	subB, err := subnetBInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer subB.Unsubscribe()

	newHeadsC := make(chan *types.Header, 10)
	subC, err := subnetCInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsC)
	Expect(err).Should(BeNil())
	defer subC.Unsubscribe()

	// Send 5 transactions to produce 5 blocks on subnet A
	// We expect exactly one of the block hashes to be published by the relayer
	subnetAHashes := []common.Hash{}
	for i := 0; i < 5; i++ {
		nonceA, err := subnetAInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
		Expect(err).Should(BeNil())
		value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(1)) // 1eth
		txA := types.NewTx(&types.DynamicFeeTx{
			ChainID:   subnetAInfo.ChainIDInt,
			Nonce:     nonceA,
			To:        &destinationAddress,
			Gas:       teleporterTestUtils.DefaultTeleporterTransactionGas,
			GasFeeCap: gasFeeCapA,
			GasTipCap: gasTipCapA,
			Value:     value,
		})
		txSignerA := types.LatestSignerForChainID(subnetAInfo.ChainIDInt)

		triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
		Expect(err).Should(BeNil())

		receipt := teleporterTestUtils.SendTransactionAndWaitForAcceptance(ctx, subnetAInfo.ChainWSClient, triggerTxA)

		log.Info("Sent block on destination", "blockHash", receipt.BlockHash)
		subnetAHashes = append(subnetAHashes, receipt.BlockHash)
	}

	// Listen on the destination chains for the published  block hash
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		receiveBlockHash(ctx, blockHashReceiverAddressB, newHeadsB, subnetBInfo, blockHashABI, subnetAHashes)
	}()
	go func() {
		defer wg.Done()
		receiveBlockHash(ctx, blockHashReceiverAddressC, newHeadsC, subnetCInfo, blockHashABI, subnetAHashes)
	}()
	wg.Wait()

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()
}
