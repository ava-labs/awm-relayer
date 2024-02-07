// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"encoding/hex"
	"os"
	"testing"

	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ava-labs/teleporter/tests/local"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	warpGenesisFile = "./tests/utils/warp-genesis.json"
)

var (
	localNetworkInstance *local.LocalNetwork
)

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Relayer e2e test")
}

// Define the Relayer before and after suite functions.
var _ = ginkgo.BeforeSuite(func() {
	localNetworkInstance = local.NewLocalNetwork(warpGenesisFile)
	// Generate the Teleporter deployment values
	teleporterContractAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterMessengerContractAddress.txt"))
	teleporterDeployerAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerAddress.txt"))
	teleporterDeployerTransactionStr := testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerTransaction.txt")
	teleporterDeployerTransaction, err := hex.DecodeString(utils.SanitizeHexString(teleporterDeployerTransactionStr))
	Expect(err).Should(BeNil())

	_, fundedKey := localNetworkInstance.GetFundedAccountInfo()
	localNetworkInstance.DeployTeleporterContracts(
		teleporterDeployerTransaction,
		teleporterDeployerAddress,
		teleporterContractAddress,
		fundedKey,
		true,
	)
	log.Info("Deployed Teleporter contracts")
	localNetworkInstance.DeployTeleporterRegistryContracts(teleporterContractAddress, fundedKey)
	log.Info("Set up ginkgo before suite")
})

var _ = ginkgo.AfterSuite(func() {
	localNetworkInstance.TearDownNetwork()
})

var _ = ginkgo.Describe("[AWM Relayer Integration Tests", func() {
	ginkgo.It("Manually Provided Message", func() {
		ManualMessage(localNetworkInstance)
	})
	ginkgo.It("Basic Relay", func() {
		BasicRelay(localNetworkInstance)
	})
	ginkgo.It("Teleporter Registry", func() {
		TeleporterRegistry(localNetworkInstance)
	})
})
