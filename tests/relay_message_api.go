// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/awm-relayer/relayer"

	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

func RelayMessageAPI(network interfaces.LocalNetwork) {
	ctx := context.Background()
	subnetAInfo := network.GetPrimaryNetworkInfo()
	subnetBInfo, _ := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()
	teleporterContractAddress := network.GetTeleporterContractAddress()
	err := testUtils.ClearRelayerStorage()
	Expect(err).Should(BeNil())

	log.Info("Funding relayer address on all subnets")
	relayerKey, err := crypto.GenerateKey()
	Expect(err).Should(BeNil())
	testUtils.FundRelayers(ctx, []interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo}, fundedKey, relayerKey)

	log.Info("Sending teleporter messages")
	receipt, _, _ := testUtils.SendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
	warpMessage := getWarpMessageFromLog(ctx, receipt, subnetAInfo)

	// Set up relayer config
	relayerConfig := testUtils.CreateDefaultRelayerConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
		teleporterContractAddress,
		fundedAddress,
		relayerKey,
	)
	// Don't process missed blocks, so we can manually relay
	relayerConfig.ProcessMissedBlocks = false

	relayerConfigPath := testUtils.WriteRelayerConfig(relayerConfig, testUtils.DefaultRelayerCfgFname)

	log.Info("Starting the relayer")
	relayerCleanup := testUtils.BuildAndRunRelayerExecutable(ctx, relayerConfigPath)
	defer relayerCleanup()

	// Sleep for some time to make sure relayer has started up and subscribed.
	log.Info("Waiting for the relayer to start up")
	time.Sleep(15 * time.Second)

	reqBody := relayer.RelayMessageRequest{
		BlockchainID: subnetAInfo.BlockchainID.String(),
		MessageID:    warpMessage.ID().Hex(),
		BlockNum:     receipt.BlockNumber.String(),
	}

	b, err := json.Marshal(reqBody)
	Expect(err).Should(BeNil())
	bodyReader := bytes.NewReader(b)

	requestURL := fmt.Sprintf("http://localhost:%d/relay-message", relayerConfig.APIPort)
	req, err := http.NewRequest(http.MethodPost, requestURL, bodyReader)
	Expect(err).Should(BeNil())
	req.Header.Set("Content-Type", "application/json")

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	res, err := client.Do(req)
	Expect(err).Should(BeNil())
	Expect(res.Status).Should(Equal("200 OK"))

	// Cancel the command and stop the relayer
	relayerCleanup()
}
