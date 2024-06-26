// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// This tests the basic functionality of the relayer using the Warp API/, rather than app requests. Includes:
// - Relaying from Subnet A to Subnet B
// - Relaying from Subnet B to Subnet A
func WarpAPIRelay(network interfaces.LocalNetwork) {
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
	// Enable the Warp API for all source blockchains
	for _, subnet := range relayerConfig.SourceBlockchains {
		subnet.WarpAPIEndpoint = subnet.RPCEndpoint
	}

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

	//
	// Verify the messages were signed using the Warp API
	//
	log.Info("Verifying the messages were signed using the Warp API")
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", relayerConfig.MetricsPort))
	Expect(err).Should(BeNil())

	body, err := io.ReadAll(resp.Body)
	Expect(err).Should(BeNil())
	defer resp.Body.Close()

	metricName := "app_fetch_signature_rpc_count"
	var totalCount uint64
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, metricName) {
			log.Info("Found metric line", "metric", line)
			parts := strings.Fields(line)

			// Fetch the metric count from the last field of the line
			value, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
			if err != nil {
				continue
			}
			totalCount += value
		}
	}
	Expect(totalCount).Should(Equal(uint64(2)))

	log.Info("Finished sending warp message, closing down output channel")
	// Cancel the command and stop the relayer
	relayerCleanup()
}
