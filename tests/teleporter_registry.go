package tests

import (
	"context"
	"encoding/hex"
	"math/big"

	runner_sdk "github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/awm-relayer/config"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
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

// Tests relayer support for off-chain Teleporter Registry updates
// - Configures the relayer to send an off-chain message to the Teleporter Registry
// - Verifies that the Teleporter Registry is updated
func TeleporterRegistry(network interfaces.LocalNetwork) {
	cChainInfo := network.GetPrimaryNetworkInfo()
	subnetAInfo, subnetBInfo := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Get the current Teleporter Registry version
	//
	currentVersion, err := cChainInfo.TeleporterRegistry.LatestVersion(&bind.CallOpts{})
	Expect(err).Should(BeNil())
	expectedNewVersion := currentVersion.Add(currentVersion, big.NewInt(1))

	//
	// Fund the relayer address on all subnets
	//
	ctx := context.Background()

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{cChainInfo}, fundedKey, relayerKey)

	//
	// Define the off-chain Warp message
	//
	log.Info("Creating off-chain Warp message")
	newProtocolAddress := common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567")
	networkID := network.GetNetworkID()

	//
	// Set up the nodes to accept the off-chain message
	//
	// Create chain config file with off chain message for each chain
	unsignedMessage, warpEnabledChainConfigC := utils.InitChainConfig(networkID, cChainInfo, newProtocolAddress)
	_, warpEnabledChainConfigA := utils.InitChainConfig(networkID, subnetAInfo, newProtocolAddress)
	_, warpEnabledChainConfigB := utils.InitChainConfig(networkID, subnetBInfo, newProtocolAddress)

	// Create chain config with off chain messages
	chainConfigs := make(map[string]string)
	utils.SetChainConfig(chainConfigs, cChainInfo, warpEnabledChainConfigC)
	utils.SetChainConfig(chainConfigs, subnetBInfo, warpEnabledChainConfigB)
	utils.SetChainConfig(chainConfigs, subnetAInfo, warpEnabledChainConfigA)

	// Restart nodes with new chain config
	nodeNames := network.GetAllNodeNames()
	log.Info("Restarting nodes with new chain config")
	network.RestartNodes(ctx, nodeNames, runner_sdk.WithChainConfigs(chainConfigs))
	// Refresh the subnet info to get the new clients
	cChainInfo = network.GetPrimaryNetworkInfo()

	//
	// Set up relayer config
	//
	relayerConfig := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{cChainInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)
	relayerConfig.ManualWarpMessages = []config.ManualWarpMessage{
		{
			UnsignedMessageBytes:    hex.EncodeToString(unsignedMessage.Bytes()),
			SourceBlockchainID:      cChainInfo.BlockchainID.String(),
			DestinationBlockchainID: cChainInfo.BlockchainID.String(),
			SourceAddress:           offchainregistry.OffChainRegistrySourceAddress.Hex(),
			DestinationAddress:      cChainInfo.TeleporterRegistryAddress.Hex(),
		},
	}
	relayerConfigPath := writeRelayerConfig(relayerConfig)
	//
	// Run the Relayer. On startup, we should deliver the message provided in the config
	//

	// Subscribe to the destination chain
	newHeadsC := make(chan *types.Header, 10)
	sub, err := cChainInfo.WSClient.SubscribeNewHead(ctx, newHeadsC)
	Expect(err).Should(BeNil())
	defer sub.Unsubscribe()

	log.Info("Starting the relayer")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	log.Info("Waiting for a new block confirmation on the C-Chain")
	<-newHeadsC

	log.Info("Verifying that the Teleporter Registry was updated")
	newVersion, err := cChainInfo.TeleporterRegistry.LatestVersion(&bind.CallOpts{})
	Expect(err).Should(BeNil())
	Expect(newVersion.Cmp(expectedNewVersion)).Should(Equal(0))
}
