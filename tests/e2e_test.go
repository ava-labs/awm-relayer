// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
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
	warpGenesisTemplateFile = "./tests/utils/warp-genesis-template.json"
)

var (
	localNetworkInstance *local.LocalNetwork

	decider       *exec.Cmd
	cancelDecider context.CancelFunc
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
	localNetworkInstance = local.NewLocalNetwork(
		"awm-relayer-e2e-test",
		warpGenesisTemplateFile,
		[]local.SubnetSpec{
			{
				Name:       "A",
				EVMChainID: 12345,
				NodeCount:  5,
			},
			{
				Name:       "B",
				EVMChainID: 54321,
				NodeCount:  5,
			},
		},
		0,
	)
	// Generate the Teleporter deployment values
	teleporterContractAddress := common.HexToAddress(
		testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterMessengerContractAddress.txt"),
	)
	teleporterDeployerAddress := common.HexToAddress(
		testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerAddress.txt"),
	)
	teleporterDeployerTransactionStr := testUtils.ReadHexTextFile(
		"./tests/utils/UniversalTeleporterDeployerTransaction.txt",
	)
	teleporterDeployerTransaction, err := hex.DecodeString(
		utils.SanitizeHexString(teleporterDeployerTransactionStr),
	)
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
	localNetworkInstance.DeployTeleporterRegistryContracts(
		teleporterContractAddress,
		fundedKey,
	)

	var ctx context.Context
	ctx, cancelDecider = context.WithCancel(context.Background())
	// we'll call cancelDecider in AfterSuite, but also call it if this
	// process is killed, because AfterSuite won't always run then:
	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		cancelDecider()
	}()
	decider = exec.CommandContext(ctx, "./tests/cmd/decider/decider")
	decider.Start()
	log.Info("Started decider service")

	log.Info("Set up ginkgo before suite")

	ginkgo.AddReportEntry(
		"network directory with node logs & configs; useful in the case of failures",
		localNetworkInstance.Dir(),
		ginkgo.ReportEntryVisibilityFailureOrVerbose,
	)
})

var _ = ginkgo.AfterSuite(func() {
	if localNetworkInstance != nil {
		localNetworkInstance.TearDownNetwork()
	}
	if decider != nil {
		cancelDecider()
	}
})

var _ = ginkgo.Describe("[AWM Relayer Integration Tests", func() {
	ginkgo.It("Manually Provided Message", func() {
		ManualMessage(localNetworkInstance)
	})
	ginkgo.It("Basic Relay", func() {
		BasicRelay(localNetworkInstance)
	})
	ginkgo.It("Shared Database", func() {
		SharedDatabaseAccess(localNetworkInstance)
	})
	ginkgo.It("Allowed Addresses", func() {
		AllowedAddresses(localNetworkInstance)
	})
	ginkgo.It("Batch Message", func() {
		BatchRelay(localNetworkInstance)
	})
	ginkgo.It("Relay Message API", func() {
		RelayMessageAPI(localNetworkInstance)
	})
	ginkgo.It("Warp API", func() {
		WarpAPIRelay(localNetworkInstance)
	})
})
