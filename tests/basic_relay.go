package tests

import (
	"context"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/database"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// This tests the basic functionality of the relayer, including:
// - Relaying from Subnet A to Subnet B
// - Relaying from Subnet B to Subnet A
// - Relaying an already delivered message
// - Setting ProcessHistoricalBlocksFromHeight in config
func BasicRelay(network interfaces.LocalNetwork) {
	subnetAInfo := network.GetPrimaryNetworkInfo()
	subnetBInfo, _ := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Fund the relayer address on all subnets
	//
	ctx := context.Background()

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

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayer")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayer to start up")
	time.Sleep(15 * time.Second)

	log.Info("Sending transaction from Subnet A to Subnet B")
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)

	//
	// Test Relaying from Subnet B to Subnet A
	//
	log.Info("Test Relaying from Subnet B to Subnet A")
	testUtils.RelayBasicMessage(
		ctx,
		subnetBInfo,
		subnetAInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)

	log.Info("Finished sending warp message, closing down output channel")
	// Cancel the command and stop the relayer
	relayerCleanup()

	//
	// Try Relaying Already Delivered Message
	//
	log.Info("Test Relaying Already Delivered Message")
	logger := logging.NewLogger(
		"awm-relayer",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)
	jsonDB, err := database.NewJSONFileStorage(logger, relayerConfig.StorageLocation, database.GetConfigRelayerIDs(&relayerConfig))
	Expect(err).Should(BeNil())

	// Create relayer keys that allow all source and destination addresses
	relayerIDA := database.CalculateRelayerID(subnetAInfo.BlockchainID, subnetBInfo.BlockchainID, database.AllAllowedAddress, database.AllAllowedAddress)
	relayerIDB := database.CalculateRelayerID(subnetBInfo.BlockchainID, subnetAInfo.BlockchainID, database.AllAllowedAddress, database.AllAllowedAddress)
	// Modify the JSON database to force the relayer to re-process old blocks
	jsonDB.Put(relayerIDA, database.LatestProcessedBlockKey, []byte("0"))
	jsonDB.Put(relayerIDB, database.LatestProcessedBlockKey, []byte("0"))

	// Subscribe to the destination chain
	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	// Run the relayer
	log.Info("Creating new relayer instance to test already delivered message")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// We should not receive a new block on subnet B, since the relayer should have seen the Teleporter message was already delivered
	log.Info("Waiting for 10s to ensure no new block confirmations on destination chain")
	Consistently(newHeadsB, 10*time.Second, 500*time.Millisecond).ShouldNot(Receive())

	//
	// Set ProcessHistoricalBlocksFromHeight in config
	//
	log.Info("Test Setting ProcessHistoricalBlocksFromHeight in config")
	testUtils.TriggerProcessMissedBlocks(
		ctx,
		subnetAInfo,
		subnetBInfo,
		relayerCleanup,
		relayerConfig,
		fundedAddress,
		fundedKey,
	)
}
