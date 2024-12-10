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

	"github.com/ava-labs/icm-contracts/tests/interfaces"
	"github.com/ava-labs/icm-contracts/tests/network"
	"github.com/ava-labs/icm-contracts/tests/utils"
	testUtils "github.com/ava-labs/icm-services/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// Fully formed name of the metric that tracks the number aggregate signatures fetched from the Warp API
const rpcSignatureMetricName = "app_fetch_signature_rpc_count"

// This tests the basic functionality of the relayer using the Warp API/, rather than app requests. Includes:
// - Relaying from Subnet A to Subnet B
// - Relaying from Subnet B to Subnet A
// - Verifying the messages were signed using the Warp API
func WarpAPIRelay(network *network.LocalNetwork, teleporter utils.TeleporterTestInfo) {
	l1AInfo := network.GetPrimaryNetworkInfo()
	l1BInfo, _ := network.GetTwoL1s()
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	//
	// Fund the relayer address on all subnets
	//
	ctx := context.Background()

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

	log.Info("Sending transaction from Subnet A to Subnet B")
	testUtils.RelayBasicMessage(
		ctx,
		teleporter,
		l1AInfo,
		l1BInfo,
		fundedKey,
		fundedAddress,
	)

	//
	// Test Relaying from Subnet B to Subnet A
	//
	log.Info("Test Relaying from Subnet B to Subnet A")
	testUtils.RelayBasicMessage(
		ctx,
		teleporter,
		l1BInfo,
		l1AInfo,
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

	var totalCount uint64
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, rpcSignatureMetricName) {
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
