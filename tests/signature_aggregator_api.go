// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/awm-relayer/signature-aggregator/api"
	"github.com/ava-labs/awm-relayer/signature-aggregator/metrics"
	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

// Tests basic functionality of the Signature Aggregator API
// Setup step:
// - Sets up a primary network and a subnet.
// - Builds and runs a signature aggregator executable.
// Test Case 1:
// - Sends a teleporter message from the primary network to the subnet.
// - Reads the warp message unsigned bytes from the log
// - Sends the unsigned message to the signature aggregator API
// - Confirms that the signed message is returned and matches the originally sent message
func SignatureAggregatorAPI(network interfaces.LocalNetwork) {
	// Begin Setup step
	ctx := context.Background()

	subnetAInfo := network.GetPrimaryNetworkInfo()
	subnetBInfo, _ := utils.GetTwoSubnets(network)
	fundedAddress, fundedKey := network.GetFundedAccountInfo()

	signatureAggregatorConfig := testUtils.CreateDefaultSignatureAggregatorConfig(
		[]interfaces.SubnetTestInfo{subnetAInfo, subnetBInfo},
	)

	signatureAggregatorConfigPath := testUtils.WriteSignatureAggregatorConfig(
		signatureAggregatorConfig,
		testUtils.DefaultSignatureAggregatorCfgFname,
	)
	log.Info("Starting the signature aggregator", "configPath", signatureAggregatorConfigPath)
	signatureAggregatorCancel, readyChan := testUtils.RunSignatureAggregatorExecutable(
		ctx,
		signatureAggregatorConfigPath,
		signatureAggregatorConfig,
	)
	defer signatureAggregatorCancel()

	// Wait for signature-aggregator to start up
	log.Info("Waiting for the relayer to start up")
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startupCancel()
	testUtils.WaitForChannelClose(startupCtx, readyChan)

	// End setup step
	// Begin Test Case 1

	log.Info("Sending teleporter message")
	receipt, _, _ := testUtils.SendBasicTeleporterMessage(
		ctx,
		subnetAInfo,
		subnetBInfo,
		fundedKey,
		fundedAddress,
	)
	warpMessage := getWarpMessageFromLog(ctx, receipt, subnetAInfo)

	reqBody := api.AggregateSignatureRequest{
		UnsignedMessage: "0x" + hex.EncodeToString(warpMessage.Bytes()),
	}

	client := http.Client{
		Timeout: 20 * time.Second,
	}

	requestURL := fmt.Sprintf("http://localhost:%d%s", signatureAggregatorConfig.APIPort, api.APIPath)

	var sendRequestToAPI = func() {
		b, err := json.Marshal(reqBody)
		Expect(err).Should(BeNil())
		bodyReader := bytes.NewReader(b)

		req, err := http.NewRequest(http.MethodPost, requestURL, bodyReader)
		Expect(err).Should(BeNil())
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		Expect(err).Should(BeNil())
		Expect(res.Status).Should(Equal("200 OK"))
		Expect(res.Header.Get("Content-Type")).Should(Equal("application/json"))

		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		Expect(err).Should(BeNil())

		var response api.AggregateSignatureResponse
		err = json.Unmarshal(body, &response)
		Expect(err).Should(BeNil())

		decodedMessage, err := hex.DecodeString(response.SignedMessage)
		Expect(err).Should(BeNil())

		signedMessage, err := avalancheWarp.ParseMessage(decodedMessage)
		Expect(err).Should(BeNil())
		Expect(signedMessage.ID()).Should(Equal(warpMessage.ID()))
	}

	sendRequestToAPI()

	// Check metrics
	metricsSample := sampleMetrics(signatureAggregatorConfig.MetricsPort)
	for _, m := range []struct {
		name  string
		op    string
		value int
	}{
		{metrics.Opts.AggregateSignaturesRequestCount.Name, "==", 1},
		{metrics.Opts.AggregateSignaturesLatencyMS.Name, ">", 0},
		{metrics.Opts.AppRequestCount.Name, "<=", 5},
		{metrics.Opts.FailuresToGetValidatorSet.Name, "==", 0},
		{metrics.Opts.FailuresToConnectToSufficientStake.Name, "==", 0},
		{metrics.Opts.FailuresSendingToNode.Name, "<", 5},
		{metrics.Opts.ValidatorTimeouts.Name, "==", 0},
		{metrics.Opts.InvalidSignatureResponses.Name, "==", 0},
		{metrics.Opts.SignatureCacheHits.Name, "==", 0},
		{metrics.Opts.SignatureCacheMisses.Name, "==", 0},
		{
			fmt.Sprintf(
				"%s{subnetID=\"%s\"}",
				metrics.Opts.ConnectedStakeWeightPercentage.Name,
				subnetAInfo.SubnetID.String(),
			),
			"==",
			100,
		},
	} {
		Expect(metricsSample[m.name]).Should(
			BeNumerically(m.op, m.value),
			"Expected metric %s %s %d",
			m.name,
			m.op,
			m.value,
		)
	}

	// make a second request, and ensure that the metrics reflect that the
	// signatures for the second request are retrieved from the cache. note
	// that even though 4 signatures were requested in the previous
	// request, only 3 will be cached, because that's all that was required
	// to reach a quorum, so that's all that were handled.
	sendRequestToAPI()
	metricsSample2 := sampleMetrics(signatureAggregatorConfig.MetricsPort)
	Expect(
		metricsSample2[metrics.Opts.AppRequestCount.Name],
	).Should(Equal(metricsSample[metrics.Opts.AppRequestCount.Name]))
	Expect(
		metricsSample2[metrics.Opts.SignatureCacheHits.Name],
	).Should(BeNumerically("==", 3))
	Expect(
		metricsSample2[metrics.Opts.SignatureCacheMisses.Name],
	).Should(Equal(metricsSample[metrics.Opts.SignatureCacheMisses.Name]))
}

// returns a map of metric names to metric samples
func sampleMetrics(port uint16) map[string]uint64 {
	resp, err := http.Get(
		fmt.Sprintf("http://localhost:%d/metrics", port),
	)
	Expect(err).Should(BeNil())

	body, err := io.ReadAll(resp.Body)
	Expect(err).Should(BeNil())
	defer resp.Body.Close()

	var samples = make(map[string]uint64)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		for _, metricName := range []string{
			metrics.Opts.AggregateSignaturesLatencyMS.Name,
			metrics.Opts.AggregateSignaturesRequestCount.Name,
			metrics.Opts.AppRequestCount.Name,
			metrics.Opts.FailuresToGetValidatorSet.Name,
			metrics.Opts.FailuresToConnectToSufficientStake.Name,
			metrics.Opts.FailuresSendingToNode.Name,
			metrics.Opts.ValidatorTimeouts.Name,
			metrics.Opts.InvalidSignatureResponses.Name,
			metrics.Opts.SignatureCacheHits.Name,
			metrics.Opts.SignatureCacheMisses.Name,
			metrics.Opts.ConnectedStakeWeightPercentage.Name,
		} {
			if strings.HasPrefix(
				line,
				"U__signature_2d_aggregator_"+metricName,
			) {
				log.Debug("Found metric line", "line", line)
				parts := strings.Fields(line)

				metricName = strings.Replace(parts[0], "U__signature_2d_aggregator_", "", 1)

				// Parse the metric count from the last field of the line
				value, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
				if err != nil {
					log.Warn("failed to parse value from metric line")
					continue
				}
				log.Debug("parsed metric", "name", metricName, "value", value)

				samples[metricName] = value
			} else {
				log.Debug("Ignoring non-metric line", "line", line)
			}
		}
	}
	return samples
}
