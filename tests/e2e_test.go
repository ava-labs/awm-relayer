// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"encoding/hex"
	"os"
	"testing"

	testUtils "github.com/ava-labs/awm-relayer/tests/utils"
	"github.com/ava-labs/awm-relayer/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	warpGenesisFile = "./tests/utils/warp-genesis.json"
)

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Relayer e2e test")
}

// Define the Relayer before and after suite functions.
var _ = ginkgo.BeforeSuite(setupSuite)

var _ = ginkgo.AfterSuite(teleporterTestUtils.TearDownNetwork)

var _ = ginkgo.Describe("[AWM Relayer Integration Tests", func() {
	ginkgo.It("Basic Relay", BasicRelay)
})

// Sets up the warp-enabled network and deploys the teleporter contract to each of the subnets
func setupSuite() {
	teleporterTestUtils.SetupNetwork(warpGenesisFile)
	teleporterContractAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterMessengerContractAddress.txt"))
	teleporterDeployerAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerAddress.txt"))
	teleporterDeployerTransactionStr := testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerTransaction.txt")
	teleporterDeployerTransaction, err := hex.DecodeString(utils.SanitizeHexString(teleporterDeployerTransactionStr))
	Expect(err).Should(BeNil())
	teleporterTestUtils.DeployTeleporterContract(teleporterDeployerTransaction, teleporterDeployerAddress, teleporterContractAddress)
}
