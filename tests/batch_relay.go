package tests

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
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

	numMessages := 50
	sentMessages := set.NewSet[string](numMessages)
	for i := 0; i < numMessages; i++ {
		sentMessages.Add(strconv.Itoa(i))
	}

	optsA, err := bind.NewKeyedTransactorWithChainID(fundedKey, subnetAInfo.EVMChainID)
	Expect(err).Should(BeNil())
	tx, err := batchMessengerA.SendMessages(
		optsA,
		subnetBInfo.BlockchainID,
		batchMessengerAddressB,
		common.Address{},
		big.NewInt(0),
		big.NewInt(int64(300000*numMessages)),
		sentMessages.List(),
	)
	Expect(err).Should(BeNil())

	utils.WaitForTransactionSuccess(ctx, subnetAInfo, tx.Hash())

	// Wait for the message on the destination
	maxWait := 30
	currWait := 0
	log.Info("Waiting to receive all messages on destination...")
	for {
		receivedMessages, err := batchMessengerB.GetCurrentMessages(&bind.CallOpts{}, subnetAInfo.BlockchainID)
		Expect(err).Should(BeNil())

		// Remove the received messages from the set of sent messages
		sentMessages.Remove(receivedMessages...)
		if sentMessages.Len() == 0 {
			break
		}
		currWait++
		if currWait == maxWait {
			Expect(false).Should(BeTrue(), fmt.Sprintf("did not receive all sent messages in time. received %d/%d", numMessages-sentMessages.Len(), numMessages))
		}
		time.Sleep(1 * time.Second)
	}
}
