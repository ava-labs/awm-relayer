package tests

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/icm-contracts/tests/interfaces"
	"github.com/ava-labs/icm-contracts/tests/network"
	"github.com/ava-labs/icm-contracts/tests/utils"
	testUtils "github.com/ava-labs/icm-services/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// Processes multiple Warp messages contained in the same block
func BatchRelay(network *network.LocalNetwork, teleporter utils.TeleporterTestInfo) {
	l1AInfo, l1BInfo := network.GetTwoL1s()
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Deploy the batch messenger contracts
	//
	ctx := context.Background()
	_, batchMessengerA := testUtils.DeployBatchCrossChainMessenger(
		ctx,
		fundedKey,
		teleporter,
		fundedAddress,
		l1AInfo,
	)
	batchMessengerAddressB, batchMessengerB := testUtils.DeployBatchCrossChainMessenger(
		ctx,
		fundedKey,
		teleporter,
		fundedAddress,
		l1BInfo,
	)

	//
	// Fund the relayer address on all subnets
	//

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.L1TestInfo{l1AInfo, l1BInfo}, fundedKey, relayerKey)

	//
	// Set up relayer config
	//
	relayerConfig := testUtils.CreateDefaultRelayerConfig(
		teleporter,
		[]interfaces.L1TestInfo{l1AInfo, l1BInfo},
		[]interfaces.L1TestInfo{l1AInfo, l1BInfo},
		fundedAddress,
		relayerKey,
	)

	relayerConfigPath := testUtils.WriteRelayerConfig(relayerConfig, testUtils.DefaultRelayerCfgFname)

	log.Info("Starting the relayer")
	relayerCleanup, readyChan := testUtils.RunRelayerExecutable(
		ctx,
		relayerConfigPath,
		relayerConfig,
	)
	defer relayerCleanup()

	// Wait for relayer to start up
	log.Info("Waiting for the relayer to start up")
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()
	testUtils.WaitForChannelClose(startupCtx, readyChan)

	//
	// Send a batch message from subnet A -> B
	//

	newHeadsDest := make(chan *types.Header, 10)
	sub, err := l1BInfo.WSClient.SubscribeNewHead(ctx, newHeadsDest)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	numMessages := 40
	sentMessages := set.NewSet[string](numMessages)
	for i := 0; i < numMessages; i++ {
		sentMessages.Add(strconv.Itoa(i))
	}

	optsA, err := bind.NewKeyedTransactorWithChainID(fundedKey, l1AInfo.EVMChainID)
	Expect(err).Should(BeNil())
	tx, err := batchMessengerA.SendMessages(
		optsA,
		l1BInfo.BlockchainID,
		batchMessengerAddressB,
		common.Address{},
		big.NewInt(0),
		big.NewInt(int64(300000*numMessages)),
		sentMessages.List(),
	)
	Expect(err).Should(BeNil())

	utils.WaitForTransactionSuccess(ctx, l1AInfo, tx.Hash())

	// Wait for the message on the destination
	maxWait := 30
	currWait := 0
	log.Info("Waiting to receive all messages on destination...")
	for {
		receivedMessages, err := batchMessengerB.GetCurrentMessages(&bind.CallOpts{}, l1AInfo.BlockchainID)
		Expect(err).Should(BeNil())

		// Remove the received messages from the set of sent messages
		sentMessages.Remove(receivedMessages...)
		if sentMessages.Len() == 0 {
			break
		}
		currWait++
		if currWait == maxWait {
			Expect(false).Should(BeTrue(),
				fmt.Sprintf(
					"did not receive all sent messages in time. received %d/%d",
					numMessages-sentMessages.Len(),
					numMessages,
				),
			)
		}
		time.Sleep(1 * time.Second)
	}
}
