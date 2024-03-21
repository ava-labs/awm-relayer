package tests

import (
	"context"
	"time"

	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

const relayerCfgFname1 = "relayer-config-1.json"
const relayerCfgFname2 = "relayer-config-2.json"

func SharedDatabaseAccess(network interfaces.LocalNetwork) {

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
	relayerKey1, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	relayerKey2, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())

	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey1)
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey2)

	//
	// Set up relayer config
	//
	// Relayer 1 will relay messages from Subnet A to Subnet B
	relayerConfig1 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo},
		[]interfaces.SubnetTestInfo{subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey1,
	)
	// Relayer 2 will relay messages from Subnet B to Subnet A
	relayerConfig2 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey2,
	)
	relayerConfig2.APIPort = 8081
	relayerConfig2.MetricsPort = 9091

	relayerConfigPath1 := testUtils.WriteRelayerConfig(relayerConfig1, relayerCfgFname1)
	relayerConfigPath2 := testUtils.WriteRelayerConfig(relayerConfig2, relayerCfgFname2)

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayers")
	relayerCleanup1 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath1)
	defer relayerCleanup1()
	relayerCleanup2 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath2)
	defer relayerCleanup2()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
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

	log.Info("Finished sending warp messages.")

	// Test processing missed blocks on both relayers.
	log.Info("Testing processing missed blocks on Subnet A")
	testUtils.TriggerProcessMissedBlocks(
		ctx,
		subnetAInfo,
		subnetBInfo,
		relayerCleanup1,
		relayerConfig1,
		fundedAddress,
		fundedKey,
	)

	log.Info("Testing processing missed blocks on Subnet B")
	testUtils.TriggerProcessMissedBlocks(
		ctx,
		subnetBInfo,
		subnetAInfo,
		relayerCleanup2,
		relayerConfig2,
		fundedAddress,
		fundedKey,
	)
}
