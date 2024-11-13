package tests

import (
	"context"
	"sync"

	testUtils "github.com/ava-labs/icm-relayer/tests/utils"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

const relayerCfgFnameA = "relayer-config-a.json"
const relayerCfgFnameB = "relayer-config-b.json"

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
	relayerKeyA, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	relayerKeyB, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())

	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKeyA)
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKeyB)

	//
	// Set up relayer config
	//
	// Relayer A will relay messages from Subnet A to Subnet B
	relayerConfigA := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo},
		[]interfaces.SubnetTestInfo{subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKeyA,
	)
	// Relayer B will relay messages from Subnet B to Subnet A
	relayerConfigB := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKeyB,
	)
	relayerConfigB.APIPort = 8081
	relayerConfigB.MetricsPort = 9091

	relayerConfigPathA := testUtils.WriteRelayerConfig(relayerConfigA, relayerCfgFnameA)
	relayerConfigPathB := testUtils.WriteRelayerConfig(relayerConfigB, relayerCfgFnameB)

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayers")
	relayerCleanupA, readyChanA := testUtils.RunRelayerExecutable(
		ctx,
		relayerConfigPathA,
		relayerConfigA,
	)
	defer relayerCleanupA()
	relayerCleanupB, readyChanB := testUtils.RunRelayerExecutable(
		ctx,
		relayerConfigPathB,
		relayerConfigB,
	)
	defer relayerCleanupB()

	// Wait for the relayers to start up
	log.Info("Waiting for the relayers to start up")
	var wg sync.WaitGroup
	wg.Add(2)
	waitFunc := func(wg *sync.WaitGroup, readyChan chan struct{}) {
		defer wg.Done()
		<-readyChan
	}
	go waitFunc(&wg, readyChanA)
	go waitFunc(&wg, readyChanB)
	wg.Wait()

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
		relayerCleanupA,
		relayerConfigA,
		fundedAddress,
		fundedKey,
	)

	log.Info("Testing processing missed blocks on Subnet B")
	testUtils.TriggerProcessMissedBlocks(
		ctx,
		subnetBInfo,
		subnetAInfo,
		relayerCleanupB,
		relayerConfigB,
		fundedAddress,
		fundedKey,
	)
}
