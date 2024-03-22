package tests

import (
	"context"
	"crypto/ecdsa"
	"time"

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

const relayerCfgFname1 = "relayer-config-1.json"
const relayerCfgFname2 = "relayer-config-2.json"
const relayerCfgFname3 = "relayer-config-3.json"
const relayerCfgFname4 = "relayer-config-4.json"

const numKeys = 4

// Tests allowed source and destination address functionality.
// First, relays messages using distinct relayer instances that all write to the same database. The instances are configured to:
// -  Deliver from any source address to any destination address
// -  Deliver from a specific source address to any destination address
// -  Deliver from any source address to a specific destination address
// -  Deliver from a specific source address to a specific destination address
// Then, checks that each relayer instance is able to properly catch up on missed messages that match its particular configuration
func AllowedAddresses(network interfaces.LocalNetwork) {
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
	relayerKey3, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	relayerKey4, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())

	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey1)
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey2)
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey3)
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey4)

	// Create a distinct key/address to be used in the configuration, and fund it
	var allowedKeys []*ecdsa.PrivateKey
	var allowedAddresses []common.Address
	var allowedAddressesStr []string
	for i := 0; i < numKeys; i++ {
		allowedKey, err := crypto.GenerateKey()
		Expect(err).Should(BeNil())
		allowedAddress := crypto.PubkeyToAddress(allowedKey.PublicKey)
		testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, allowedKey)
		allowedKeys = append(allowedKeys, allowedKey)
		allowedAddresses = append(allowedAddresses, allowedAddress)
		allowedAddressesStr = append(allowedAddressesStr, allowedAddress.String())
	}
	log.Info("Allowed addresses", "allowedAddresses", allowedAddressesStr)

	// All sources -> All destinations
	// Will send from allowed Address 0 -> 0
	relayerConfig1 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey1,
	)

	// Specific source -> All destinations
	// Will send from allowed Address 1 -> 0
	relayerConfig2 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey2,
	)
	for _, src := range relayerConfig2.SourceBlockchains {
		src.AllowedOriginSenderAddresses = []string{allowedAddresses[1].String()}
	}
	relayerConfig2.APIPort = 8081
	relayerConfig2.MetricsPort = 9091

	// All sources -> Specific destination
	// Will send from allowed Address 2 -> 0
	relayerConfig3 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey3,
	)
	for _, dst := range relayerConfig3.DestinationBlockchains {
		dst.AllowedDestinationAddresses = []string{allowedAddresses[2].String()}
	}
	relayerConfig3.APIPort = 8082
	relayerConfig3.MetricsPort = 9092

	// Specific source -> Specific destination
	// Will send from allowed Address 3 -> 0
	relayerConfig4 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey4,
	)
	for _, src := range relayerConfig4.SourceBlockchains {
		src.AllowedOriginSenderAddresses = []string{allowedAddresses[3].String()}
	}
	for _, dst := range relayerConfig4.DestinationBlockchains {
		dst.AllowedDestinationAddresses = []string{allowedAddresses[0].String()}
	}
	relayerConfig4.APIPort = 8083
	relayerConfig4.MetricsPort = 9093

	relayerConfigPath1 := testUtils.WriteRelayerConfig(relayerConfig1, relayerCfgFname1)
	relayerConfigPath2 := testUtils.WriteRelayerConfig(relayerConfig2, relayerCfgFname2)
	relayerConfigPath3 := testUtils.WriteRelayerConfig(relayerConfig3, relayerCfgFname3)
	relayerConfigPath4 := testUtils.WriteRelayerConfig(relayerConfig4, relayerCfgFname4)

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")
	// Subscribe to the destination chain
	newHeadsB := make(chan *types.Header, 10)
	sub, err := subnetBInfo.WSClient.SubscribeNewHead(ctx, newHeadsB)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	// Test Relayer 4
	log.Info("Testing Relayer 4")
	relayerCleanup4 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath4)
	defer relayerCleanup4()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 4
	_, _, id := testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[0], // not allowed
		allowedAddresses[0],
	)
	Consistently(func() bool {
		delivered, err := subnetBInfo.TeleporterMessenger.MessageReceived(
			&bind.CallOpts{}, id,
		)
		Expect(err).Should(BeNil())
		return delivered
	}, 10*time.Second, 500*time.Millisecond).Should(BeFalse())

	// Allowed by Relayer 4
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		allowedKeys[3],
		allowedAddresses[0],
	)
	relayerCleanup4()

	// Test Relayer 3
	log.Info("Testing Relayer 3")
	relayerCleanup3 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath3)
	defer relayerCleanup3()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 3
	_, _, id = testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[0],
		allowedAddresses[0], // not allowed
	)
	Consistently(func() bool {
		delivered, err := subnetBInfo.TeleporterMessenger.MessageReceived(
			&bind.CallOpts{}, id,
		)
		Expect(err).Should(BeNil())
		return delivered
	}, 10*time.Second, 500*time.Millisecond).Should(BeFalse())

	// Allowed by Relayer 3
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		allowedKeys[0],
		allowedAddresses[2],
	)
	relayerCleanup3()

	// Test Relayer 2
	log.Info("Testing Relayer 2")
	relayerCleanup2 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath2)
	defer relayerCleanup2()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 2
	_, _, id = testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[0], // not allowed
		allowedAddresses[0],
	)
	Consistently(func() bool {
		delivered, err := subnetBInfo.TeleporterMessenger.MessageReceived(
			&bind.CallOpts{}, id,
		)
		Expect(err).Should(BeNil())
		return delivered
	}, 10*time.Second, 500*time.Millisecond).Should(BeFalse())

	// Allowed by Relayer 2
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		allowedKeys[1],
		allowedAddresses[0],
	)
	relayerCleanup2()

	// Test Relayer 1
	log.Info("Testing Relayer 1")
	relayerCleanup1 := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath1)
	defer relayerCleanup1()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Allowed by Relayer 1
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		allowedKeys[0],
		allowedAddresses[0],
	)
	relayerCleanup1()

	// Check the database state to ensure that the four relayer instances wrote to distinct keys
}
