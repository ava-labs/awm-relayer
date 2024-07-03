// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/api"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/subnet-evm/core/types"
	subnetEvmInterfaces "github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
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

	log.Info("Sending teleporter message")
	receipt, _, teleporterMessageID := testUtils.SendBasicTeleporterMessage(ctx, subnetAInfo, subnetBInfo, fundedKey, fundedAddress)
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

	reqBody := api.RelayMessageRequest{
		BlockchainID: subnetAInfo.BlockchainID.String(),
		MessageID:    warpMessage.ID().String(),
		BlockNum:     receipt.BlockNumber.Uint64(),
	}

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	requestURL := fmt.Sprintf("http://localhost:%d%s", relayerConfig.APIPort, api.RelayAPIPath)

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

		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		Expect(err).Should(BeNil())

		var response api.RelayMessageResponse
		err = json.Unmarshal(body, &response)
		Expect(err).Should(BeNil())

		receipt, err := subnetBInfo.RPCClient.TransactionReceipt(ctx, common.HexToHash(response.TransactionHash))
		Expect(err).Should(BeNil())
		receiveEvent, err := teleporterTestUtils.GetEventFromLogs(receipt.Logs, subnetBInfo.TeleporterMessenger.ParseReceiveCrossChainMessage)
		Expect(err).Should(BeNil())
		Expect(ids.ID(receiveEvent.MessageID)).Should(Equal(teleporterMessageID))
	}

	// Send the same request to ensure the correct response.
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

		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		Expect(err).Should(BeNil())

		var response api.RelayMessageResponse
		err = json.Unmarshal(body, &response)
		Expect(err).Should(BeNil())
		Expect(response.TransactionHash).Should(Equal("0x0000000000000000000000000000000000000000000000000000000000000000"))
	}

	// Cancel the command and stop the relayer
	relayerCleanup()
}

func getWarpMessageFromLog(ctx context.Context, receipt *types.Receipt, source interfaces.SubnetTestInfo) *avalancheWarp.UnsignedMessage {
	log.Info("Fetching relevant warp logs from the newly produced block")
	logs, err := source.RPCClient.FilterLogs(ctx, subnetEvmInterfaces.FilterQuery{
		BlockHash: &receipt.BlockHash,
		Addresses: []common.Address{warp.Module.Address},
	})
	Expect(err).Should(BeNil())
	Expect(len(logs)).Should(Equal(1))

	// Check for relevant warp log from subscription and ensure that it matches
	// the log extracted from the last block.
	txLog := logs[0]
	log.Info("Parsing logData as unsigned warp message")
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(txLog.Data)
	Expect(err).Should(BeNil())

	return unsignedMsg
}
