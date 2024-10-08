// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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

// This tests basic functionality of the relayer post-etna upgrade
// since other tests write zero value of time.Time
// to the config  and therefore are testing the pre-etna case
// - Relaying from Subnet A to Subnet B
// - Relaying from Subnet B to Subnet A
func EtnaUpgrade(network interfaces.LocalNetwork) {
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
	// Setting the EtnaTime to yesterday so that the post-etna logic path is used.
	relayerConfig.EtnaTime = time.Now().AddDate(0, 0, -1)
	// The config needs to be validated in order to be passed to database.GetConfigRelayerIDs
	relayerConfig.Validate()

	relayerConfigPath := testUtils.WriteRelayerConfig(relayerConfig, testUtils.DefaultRelayerCfgFname)

	//
	// Test Relaying from Subnet A to Subnet B
	//
	log.Info("Test Relaying from Subnet A to Subnet B")

	log.Info("Starting the relayer")
	relayerCleanup, readyChan := testUtils.RunRelayerExecutable(
		ctx,
		relayerConfigPath,
		relayerConfig,
	)
	defer relayerCleanup()

	// Wait for relayer to start up
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()
	testUtils.WaitForChannelClose(startupCtx, readyChan)

	log.Info("Sending transaction from Subnet A to Subnet B")
	testUtils.RelayBasicMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)

	log.Info("Test Relaying from Subnet B to Subnet A")
	testUtils.RelayBasicMessage(
		ctx,
		subnetBInfo,
		subnetAInfo,
		teleporterContractAddress,
		fundedKey,
		fundedAddress,
	)
}
