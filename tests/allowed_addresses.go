package tests

import (
	"context"
	"crypto/ecdsa"
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/database"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
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
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey)

	// Create distinct key/address pairs to be used in the configuration, and fund them
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

	// Track which addresses are allowed by each relayer
	generalAllowedAddressIdx := 0
	relayer2AllowedSrcAddressIdx := 1
	relayer3AllowedDstAddressIdx := 2
	relayer4AllowedSrcAddressIdx := 3
	relayer4AllowedDstAddressIdx := 0

	//
	// Configure the relayers
	//

	// All sources -> All destinations
	// Will send from allowed Address 0 -> 0
	relayerConfig1 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)

	// Specific source -> All destinations
	// Will send from allowed Address 1 -> 0
	relayerConfig2 := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)
	for _, src := range relayerConfig2.SourceBlockchains {
		src.AllowedOriginSenderAddresses = []string{allowedAddresses[relayer2AllowedSrcAddressIdx].String()}
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
		relayerKey,
	)
	supportedDestinations := []*config.SupportedDestination{
		{
			BlockchainID: subnetAInfo.BlockchainID.String(),
			Addresses:    []string{allowedAddresses[relayer3AllowedDstAddressIdx].String()},
		},
		{
			BlockchainID: subnetBInfo.BlockchainID.String(),
			Addresses:    []string{allowedAddresses[relayer3AllowedDstAddressIdx].String()},
		},
	}
	for _, src := range relayerConfig3.SourceBlockchains {
		src.SupportedDestinations = supportedDestinations
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
		relayerKey,
	)
	for _, src := range relayerConfig4.SourceBlockchains {
		src.AllowedOriginSenderAddresses = []string{allowedAddresses[relayer4AllowedSrcAddressIdx].String()}
	}
	supportedDestinations = []*config.SupportedDestination{
		{
			BlockchainID: subnetAInfo.BlockchainID.String(),
			Addresses:    []string{allowedAddresses[relayer4AllowedDstAddressIdx].String()},
		},
		{
			BlockchainID: subnetBInfo.BlockchainID.String(),
			Addresses:    []string{allowedAddresses[relayer4AllowedDstAddressIdx].String()},
		},
	}
	for _, src := range relayerConfig4.SourceBlockchains {
		src.SupportedDestinations = supportedDestinations
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

	// Test Relayer 1
	log.Info("Testing Relayer 1: All sources -> All destinations")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath1)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Allowed by Relayer 1
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		allowedKeys[generalAllowedAddressIdx],
		allowedAddresses[generalAllowedAddressIdx],
	)
	height1, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	relayerCleanup()

	// Test Relayer 2
	log.Info("Testing Relayer 2: Specific source -> All destinations")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath2)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 2
	_, _, id := testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[generalAllowedAddressIdx], // not allowed
		allowedAddresses[generalAllowedAddressIdx],
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
		allowedKeys[relayer2AllowedSrcAddressIdx],
		allowedAddresses[generalAllowedAddressIdx],
	)
	height2, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	relayerCleanup()

	// Test Relayer 3
	log.Info("Testing Relayer 3: All sources -> Specific destination")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath3)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 3
	_, _, id = testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[generalAllowedAddressIdx],
		allowedAddresses[generalAllowedAddressIdx], // not allowed
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
		allowedKeys[generalAllowedAddressIdx],
		allowedAddresses[relayer3AllowedDstAddressIdx],
	)
	height3, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	relayerCleanup()

	// Test Relayer 4
	log.Info("Testing Relayer 4: Specific source -> Specific destination")
	relayerCleanup = testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath4)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayers to start up")
	time.Sleep(10 * time.Second)

	// Disallowed by Relayer 4
	_, _, id = testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		allowedKeys[generalAllowedAddressIdx], // not allowed
		allowedAddresses[generalAllowedAddressIdx],
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
		allowedKeys[relayer4AllowedSrcAddressIdx],
		allowedAddresses[relayer4AllowedDstAddressIdx],
	)
	height4, err := subnetAInfo.RPCClient.BlockNumber(ctx)
	Expect(err).Should(BeNil())
	relayerCleanup()

	//
	// Check the database state to ensure that the four relayer instances wrote to distinct keys
	//

	// Create relayer keys that allow all source and destination addresses
	relayerID1 := database.RelayerID{
		SourceBlockchainID:      subnetAInfo.BlockchainID,
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		OriginSenderAddress:     database.AllAllowedAddress,
		DestinationAddress:      database.AllAllowedAddress,
	}
	relayerID2 := database.RelayerID{
		SourceBlockchainID:      subnetAInfo.BlockchainID,
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		OriginSenderAddress:     allowedAddresses[relayer2AllowedSrcAddressIdx],
		DestinationAddress:      database.AllAllowedAddress,
	}
	relayerID3 := database.RelayerID{
		SourceBlockchainID:      subnetAInfo.BlockchainID,
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		OriginSenderAddress:     database.AllAllowedAddress,
		DestinationAddress:      allowedAddresses[relayer3AllowedDstAddressIdx],
	}
	relayerID4 := database.RelayerID{
		SourceBlockchainID:      subnetAInfo.BlockchainID,
		DestinationBlockchainID: subnetBInfo.BlockchainID,
		OriginSenderAddress:     allowedAddresses[relayer4AllowedSrcAddressIdx],
		DestinationAddress:      allowedAddresses[relayer4AllowedDstAddressIdx],
	}
	relayerKeys := []database.RelayerID{relayerID1, relayerID2, relayerID3, relayerID4}
	jsonDB, err := database.NewJSONFileStorage(logging.NoLog{}, testUtils.StorageLocation, relayerKeys)
	Expect(err).Should(BeNil())

	// Fetch the checkpointed heights from the shared database
	data, err := jsonDB.Get(relayerID1.GetID(), database.LatestProcessedBlockKey)
	Expect(err).Should(BeNil())
	storedHeight1, err := strconv.ParseUint(string(data), 10, 64)
	Expect(err).Should(BeNil())
	Expect(storedHeight1).Should(Equal(height1))

	data, err = jsonDB.Get(relayerID2.GetID(), database.LatestProcessedBlockKey)
	Expect(err).Should(BeNil())
	storedHeight2, err := strconv.ParseUint(string(data), 10, 64)
	Expect(err).Should(BeNil())
	Expect(storedHeight2).Should(Equal(height2))

	data, err = jsonDB.Get(relayerID3.GetID(), database.LatestProcessedBlockKey)
	Expect(err).Should(BeNil())
	storedHeight3, err := strconv.ParseUint(string(data), 10, 64)
	Expect(err).Should(BeNil())
	Expect(storedHeight3).Should(Equal(height3))

	data, err = jsonDB.Get(relayerID4.GetID(), database.LatestProcessedBlockKey)
	Expect(err).Should(BeNil())
	storedHeight4, err := strconv.ParseUint(string(data), 10, 64)
	Expect(err).Should(BeNil())
	Expect(storedHeight4).Should(Equal(height4))
}
