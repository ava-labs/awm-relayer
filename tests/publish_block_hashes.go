package tests

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	relayerEvm "github.com/ava-labs/awm-relayer/vms/evm"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/interfaces"
	teleporter_block_hash "github.com/ava-labs/teleporter/abis/go/teleporter-block-hash"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

func PublishBlockHashes() {
	var (
		relayerCmd                *exec.Cmd
		relayerCancel             context.CancelFunc
		blockHashReceiverAddressB common.Address
		subnetAHashes             []common.Hash
		blockHashABI              *abi.ABI
	)

	subnetAInfo := teleporterTestUtils.GetSubnetATestInfo()
	subnetBInfo := teleporterTestUtils.GetSubnetBTestInfo()
	fundedAddress, fundedKey := teleporterTestUtils.GetFundedAccountInfo()

	//
	// Deploy block hash receiver on Subnet B
	//
	ctx := context.Background()
	blockHashReceiverByteCode := readHexTextFile("./tests/BlockHashReceiverByteCode.txt")

	nonceB, err := subnetBInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	// gasTipCapA, err := subnetAInfo.ChainWSClient.SuggestGasTipCap(context.Background())
	// Expect(err).Should(BeNil())

	// baseFeeA, err := subnetAInfo.ChainWSClient.EstimateBaseFee(context.Background())
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
	// 	subnetBInfo.ChainWSClient,
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
		"--rpc-url", subnetBInfo.ChainWSURI,
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
	deployedCode, err := subnetBInfo.ChainWSClient.CodeAt(ctx, blockHashReceiverAddressB, nil)
	Expect(err).Should(BeNil())
	Expect(len(deployedCode)).Should(BeNumerically(">", 2)) // 0x is an EOA, contract returns the bytecode

	log.Info("Deployed block hash receiver contract", "address", blockHashReceiverAddressB.Hex())

	//
	// Setup relayer config
	//
	hostA, portA, err := getURIHostAndPort(subnetAInfo.ChainNodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := getURIHostAndPort(subnetBInfo.ChainNodeURIs[0])
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
	cmd = exec.Command("./scripts/build.sh")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	Expect(err).Should(BeNil())

	//
	// Publish block hashes
	//
	relayerCmd, relayerCancel = runRelayerExecutable(ctx)
	nonceA, err := subnetAInfo.ChainWSClient.NonceAt(ctx, fundedAddress, nil)
	Expect(err).Should(BeNil())

	destinationAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	gasTipCapA, err := subnetAInfo.ChainWSClient.SuggestGasTipCap(context.Background())
	Expect(err).Should(BeNil())

	baseFeeA, err := subnetAInfo.ChainWSClient.EstimateBaseFee(context.Background())
	Expect(err).Should(BeNil())
	gasFeeCapA := baseFeeA.Mul(baseFeeA, big.NewInt(relayerEvm.BaseFeeFactor))
	gasFeeCapA.Add(gasFeeCapA, big.NewInt(relayerEvm.MaxPriorityFeePerGas))

	// Subscribe to the destination chain block published
	newHeadsA := make(chan *types.Header, 10)
	subA, err := subnetAInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsA)
	Expect(err).Should(BeNil())
	defer subA.Unsubscribe()

	// Subscribe to the destination chain block published
	newHeadsB := make(chan *types.Header, 10)
	subB, err := subnetBInfo.ChainWSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer subB.Unsubscribe()

	// TODONOW: not necessarily true, since the block height might be < 5
	// Send 5 transactions to produce 5 blocks on subnet A
	// We expect at exactly one of the block hashes to be published by the relayer
	for i := 0; i < 5; i++ {
		value := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(1)) // 1eth
		txA := types.NewTx(&types.DynamicFeeTx{
			ChainID:   subnetAInfo.ChainIDInt,
			Nonce:     nonceA + uint64(i),
			To:        &destinationAddress,
			Gas:       defaultTeleporterMessageGas,
			GasFeeCap: gasFeeCapA,
			GasTipCap: gasTipCapA,
			Value:     value,
		})
		txSignerA := types.LatestSignerForChainID(subnetAInfo.ChainIDInt)
		triggerTxA, err := types.SignTx(txA, txSignerA, fundedKey)
		Expect(err).Should(BeNil())
		err = subnetAInfo.ChainWSClient.SendTransaction(ctx, triggerTxA)
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

		block, err := subnetBInfo.ChainWSClient.BlockByHash(ctx, blockHashB)
		Expect(err).Should(BeNil())
		txs := block.Transactions()
		log.Info(fmt.Sprintf("numTxs: %d", len(txs)))
		for _, tx := range txs {
			log.Info(fmt.Sprintf("txHash: %s", tx.Hash().String()))
			log.Info(fmt.Sprintf("to: %s", tx.To().String()))
			log.Info(fmt.Sprintf("data: %s", hex.EncodeToString(tx.Data())))
			receipt, err := subnetBInfo.ChainWSClient.TransactionReceipt(ctx, tx.Hash())
			Expect(err).Should(BeNil())
			Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))
		}

		// receipt, err := subnetBInfo.ChainWSClient.TransactionReceipt(ctx, blockHashB)
		// Expect(err).Should(BeNil())
		// Expect(receipt.Status).Should(Equal(types.ReceiptStatusSuccessful))

		logs, err := subnetBInfo.ChainWSClient.FilterLogs(ctx, interfaces.FilterQuery{
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
	// 	logs, err := subnetBInfo.ChainWSClient.FilterLogs(ctx, interfaces.FilterQuery{
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
	// 	logs, err := subnetBInfo.ChainWSClient.FilterLogs(ctx, interfaces.FilterQuery{
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

	//
	// Verify received block hash
	//
}
