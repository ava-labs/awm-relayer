package tests

import (
	"context"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/set"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// Processes multiple Warp messages contained in the same block
func BatchRelay(network interfaces.LocalNetwork) {
	subnetAInfo, subnetBInfo := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Deploy the batch messenger contracts
	//
	ctx := context.Background()
	_, batchMessengerA := testUtils.DeployBatchCrossChainMessenger(
		ctx,
		fundedKey,
		fundedAddress,
		subnetAInfo,
	)
	batchMessengerAddressB, batchMessengerB := testUtils.DeployBatchCrossChainMessenger(
		ctx,
		fundedKey,
		fundedAddress,
		subnetBInfo,
	)

	//
	// Fund the relayer address on all subnets
	//

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey)

	//
	// Set up relayer config
	//
	relayerConfig := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)

	relayerConfigPath := testUtils.WriteRelayerConfig(relayerConfig, testUtils.DefaultRelayerCfgFname)

	log.Info("Starting the relayer")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayer to start up")
	time.Sleep(15 * time.Second)

	//
	// Send a batch message from subnet A -> B
	//

	newHeadsDest := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsDest)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	messages := []string{"hello", "world", "this", "is", "a", "cross", "chain", "batch", "message"}
	sentMessages := set.NewSet[string](len(messages))
	for _, msg := range messages {
		sentMessages.Add(msg)
	}

	optsA, err := bind.NewKeyedTransactorWithChainID(fundedKey, subnetAInfo.EVMChainID)
	Expect(err).Should(BeNil())
	tx, err := batchMessengerA.SendMessages(
		optsA,
		subnetBInfo.BlockchainID,
		batchMessengerAddressB,
		common.Address{},
		big.NewInt(0),
		big.NewInt(500000),
		messages,
	)
	Expect(err).Should(BeNil())

	utils.WaitForTransactionSuccess(ctx, subnetAInfo, tx.Hash())

	// Wait for the message on the destination
	<-newHeadsDest

	_, receivedMessages, err := batchMessengerB.GetCurrentMessages(&bind.CallOpts{}, subnetAInfo.BlockchainID)
	Expect(err).Should(BeNil())
	for _, msg := range receivedMessages {
		Expect(sentMessages.Contains(msg)).To(BeTrue())
	}
}
