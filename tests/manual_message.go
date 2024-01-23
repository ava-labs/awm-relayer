package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	subnetEvmInterfaces "github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	. "github.com/onsi/gomega"
)

// This tests relaying a message manually provided in the relayer config
func ManualMessage(network interfaces.LocalNetwork) {
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
	// Send two Teleporter message on Subnet A, before the relayer is running
	//

	log.Info("Sending two teleporter messages on subnet A")
	// This message will be delivered by the relayer
	receipt1, _, id1 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	msg1 := getWarpMessageFromLog(ctx, receipt1, subnetAInfo)

	// This message will not be delivered by the relayer
	_, _, id2 := sendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)

	//
	// Set up relayer config to deliver one of the two previously sent messages
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
		LogLevel:            logging.Info.LowerString(),
		NetworkID:           peers.LocalNetworkID,
		PChainAPIURL:        subnetAInfo.NodeURIs[0],
		EncryptConnection:   false,
		StorageLocation:     storageLocation,
		ProcessMissedBlocks: false,
		ManualWarpMessages: []config.ManualWarpMessage{
			{
				UnsignedMessageBytes:    hex.EncodeToString(msg1.Bytes()),
				SourceBlockchainID:      subnetAInfo.BlockchainID.String(),
				DestinationBlockchainID: subnetBInfo.BlockchainID.String(),
				SourceAddress:           teleporterContractAddress.Hex(),
				DestinationAddress:      teleporterContractAddress.Hex(),
			},
		},
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
	// Run the Relayer. On startup, we should deliver the message provided in the config
	//

	// Subscribe to the destination chain
	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	log.Info("Starting the relayer")
	relayerCmd, relayerCancel := testUtils.RunRelayerExecutable(ctx, relayerConfigPath)

	log.Info("Waiting for a new block confirmation on subnet B")
	<-newHeadsB
	delivered1, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, id1,
	)
	Expect(err).Should(BeNil())
	Expect(delivered1).Should(BeTrue())

	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	delivered2, err := subnetBInfo.TeleporterMessenger.MessageReceived(
		&bind.CallOpts{}, id2,
	)
	Expect(err).Should(BeNil())
	Expect(delivered2).Should(BeFalse())

	// Cancel the command and stop the relayer
	relayerCancel()
	_ = relayerCmd.Wait()
}

func getWarpMessageFromLog(ctx context.Context, receipt *types.Receipt, source interfaces.SubnetTestInfo) *avalancheWarp.UnsignedMessage {
	log.Info("Fetching relevant warp logs from the newly produced block")
	logs, err := source.RPCClient.FilterLogs(ctx, subnetEvmInterfaces.FilterQuery{
		BlockHash: &receipt.BlockHash,
		Addresses: []common.Address{warp.Module.Address},
	})
	Expect(err).Should(BeNil())
	Expect(len(logs)).Should(Equal(1))

	// Check for relevant warp log from subscription and ensure that it matches
	// the log extracted from the last block.
	txLog := logs[0]
	log.Info("Parsing logData as unsigned warp message")
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(txLog.Data)
	Expect(err).Should(BeNil())

	return unsignedMsg
}
