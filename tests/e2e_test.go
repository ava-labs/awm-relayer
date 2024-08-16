// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	// In case of a panic we need to recover to ensure the Ginkgo cleanup is done
	defer func() {
		if r := recover(); r != nil {
			fmt.Sprintf("Panic caught: %v", r)
			cleanup()
		}
	}()
	// Handle SIGINT and SIGTERM signals as well.
	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		fmt.Printf("Caught signal %s: Shutting down...\n", sig)
		cleanup()
	}()

	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Environment variable RUN_E2E not set; skipping E2E tests")
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Relayer e2e test")

}

// Define the Relayer before and after suite functions.
var _ = ginkgo.BeforeSuite(func() {
	var ctx context.Context
	ctx, cancelDecider = context.WithCancel(context.Background())

	log.Info("Building all ICM off-chain service executables")
	testUtils.BuildAllExecutables(ctx)

	log.Info("Setting up local network")
	localNetworkInstance = local.NewLocalNetwork(
		"icm-off-chain-services-e2e-test",
		warpGenesisTemplateFile,
		[]local.SubnetSpec{
			{
				Name:       "A",
				EVMChainID: 12345,
				NodeCount:  2,
			},
			{
				Name:       "B",
				EVMChainID: 54321,
				NodeCount:  2,
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

	decider = exec.CommandContext(ctx, "./tests/cmd/decider/decider")
	decider.Start()
	go func() { // panic if the decider exits abnormally
		err := decider.Wait()
		// Context cancellation is the only expected way for the
		// process to exit, otherwise panic
		if !errors.Is(ctx.Err(), context.Canceled) {
			panic(fmt.Errorf("decider exited abnormally: %w", err))
		}
	}()
	log.Info("Started decider service")

	log.Info("Set up ginkgo before suite")

	ginkgo.AddReportEntry(
		"network directory with node logs & configs; useful in the case of failures",
		localNetworkInstance.Dir(),
		ginkgo.ReportEntryVisibilityFailureOrVerbose,
	)

})

func cleanup() {
	fmt.Printf("Cleaning up\n")
	if localNetworkInstance != nil {
		fmt.Printf("Started cleaning up\n")
		localNetworkInstance.TearDownNetwork()
		fmt.Printf("Finished cleaning up\n")
	}
	if decider != nil {
		cancelDecider()
	}
}

var _ = ginkgo.AfterSuite(cleanup)

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
	ginkgo.It("Signature Aggregator", func() {
		SignatureAggregatorAPI(localNetworkInstance)
	})
})
