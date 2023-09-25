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

// BeforeSuite starts the default network and adds 10 new nodes as validators with BLS keys
// registered on the P-Chain.
// Adds two disjoint sets of 5 of the new validator nodes to validate two new subnets with a
// a single Subnet-EVM blockchain.
var _ = ginkgo.BeforeSuite(func() {
	teleporterTestUtils.SetupNetwork(warpGenesisFile)
	teleporterContractAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterMessengerContractAddress.txt"))
	teleporterDeployerAddress := common.HexToAddress(testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerAddress.txt"))
	teleporterDeployerTransactionStr := testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerTransaction.txt")
	teleporterDeployerTransaction, err := hex.DecodeString(utils.SanitizeHexString(teleporterDeployerTransactionStr))
	Expect(err).Should(BeNil())
	teleporterTestUtils.DeployTeleporterContract(teleporterDeployerTransaction, teleporterDeployerAddress, teleporterContractAddress)
})

var _ = ginkgo.AfterSuite(teleporterTestUtils.TearDownNetwork)
