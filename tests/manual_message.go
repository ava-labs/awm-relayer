// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner/client"
	offchainregistry "github.com/ava-labs/awm-relayer/messages/off-chain-registry"
	"github.com/ava-labs/awm-relayer/relayer"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/teleporter/tests/interfaces"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// Tests relayer support for off-chain Teleporter Registry updates
// - Configures the relayer to send an off-chain message to the Teleporter Registry
// - Verifies that the Teleporter Registry is updated
func ManualMessage(network interfaces.LocalNetwork) {
	cChainInfo := network.GetPrimaryNetworkInfo()
	subnetAInfo, subnetBInfo := teleporterTestUtils.GetTwoSubnets(network)
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
	unsignedMessage, warpEnabledChainConfigC := teleporterTestUtils.InitOffChainMessageChainConfig(networkID, cChainInfo, newProtocolAddress, 2)
	_, warpEnabledChainConfigA := teleporterTestUtils.InitOffChainMessageChainConfig(networkID, subnetAInfo, newProtocolAddress, 2)
	_, warpEnabledChainConfigB := teleporterTestUtils.InitOffChainMessageChainConfig(networkID, subnetBInfo, newProtocolAddress, 2)

	// Create chain config with off chain messages
	chainConfigs := make(map[string]string)
	teleporterTestUtils.SetChainConfig(chainConfigs, cChainInfo, warpEnabledChainConfigC)
	teleporterTestUtils.SetChainConfig(chainConfigs, subnetBInfo, warpEnabledChainConfigB)
	teleporterTestUtils.SetChainConfig(chainConfigs, subnetAInfo, warpEnabledChainConfigA)

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
		[]interfaces.SubnetTestInfo{cChainInfo},
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

	reqBody := relayer.ManualWarpMessage{
		UnsignedMessageBytes:    unsignedMessage.Bytes(),
		SourceBlockchainID:      cChainInfo.BlockchainID,
		DestinationBlockchainID: cChainInfo.BlockchainID,
		SourceAddress:           offchainregistry.OffChainRegistrySourceAddress,
		DestinationAddress:      cChainInfo.TeleporterRegistryAddress,
	}

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	requestURL := fmt.Sprintf("http://localhost:%d%s", relayerConfig.APIPort, relayer.RelayMessageApiPath)

	// Send request to API
	{
		b, err := json.Marshal(reqBody)
		Expect(err).Should(BeNil())
		bodyReader := bytes.NewReader(b)

		req, err := http.NewRequest(http.MethodPost, requestURL, bodyReader)
		Expect(err).Should(BeNil())
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		Expect(err).Should(BeNil())
		Expect(res.Status).Should(Equal("200 OK"))

		newVersion, err := cChainInfo.TeleporterRegistry.LatesatVersion(&bind.CallOpts{})
		Expect(err).Should(BeNil())
		Expect(newVersion.Uint64()).Should(Equal(expectedNewVersion.Uint64()))
	}
}
