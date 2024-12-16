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
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/icm-contracts/tests/network"
	teleporterTestUtils "github.com/ava-labs/icm-contracts/tests/utils"
	testUtils "github.com/ava-labs/icm-services/tests/utils"
	"github.com/ava-labs/icm-services/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	warpGenesisTemplateFile = "./tests/utils/warp-genesis-template.json"
)

var (
	localNetworkInstance *network.LocalNetwork
	teleporterInfo       teleporterTestUtils.TeleporterTestInfo

	decider  *exec.Cmd
	cancelFn context.CancelFunc
)

func TestE2E(t *testing.T) {
	// Handle SIGINT and SIGTERM signals.
	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		fmt.Printf("Caught signal %s: Shutting down...\n", sig)
		cleanup()
		os.Exit(1)
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
	ctx, cancelFn = context.WithCancel(context.Background())

	log.Info("Building all ICM service executables")
	testUtils.BuildAllExecutables(ctx)

	// Generate the Teleporter deployment values
	teleporterContractAddress := common.HexToAddress(
		testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterMessengerContractAddress.txt"),
	)
	teleporterDeployerAddress := common.HexToAddress(
		testUtils.ReadHexTextFile("./tests/utils/UniversalTeleporterDeployerAddress.txt"),
	)
	teleporterDeployedByteCode := testUtils.ReadHexTextFile(
		"./tests/utils/UniversalTeleporterDeployedBytecode.txt",
	)
	teleporterDeployerTransactionStr := testUtils.ReadHexTextFile(
		"./tests/utils/UniversalTeleporterDeployerTransaction.txt",
	)
	teleporterDeployerTransaction, err := hex.DecodeString(
		utils.SanitizeHexString(teleporterDeployerTransactionStr),
	)
	Expect(err).Should(BeNil())
	networkStartCtx, networkStartCancel := context.WithTimeout(ctx, 240*2*time.Second)
	defer networkStartCancel()
	localNetworkInstance = network.NewLocalNetwork(
		networkStartCtx,
		"icm-off-chain-services-e2e-test",
		warpGenesisTemplateFile,
		[]network.L1Spec{
			{
				Name:                         "A",
				EVMChainID:                   12345,
				TeleporterContractAddress:    teleporterContractAddress,
				TeleporterDeployedBytecode:   teleporterDeployedByteCode,
				TeleporterDeployerAddress:    teleporterDeployerAddress,
				NodeCount:                    2,
				RequirePrimaryNetworkSigners: true,
			},
			{
				Name:                         "B",
				EVMChainID:                   54321,
				TeleporterContractAddress:    teleporterContractAddress,
				TeleporterDeployedBytecode:   teleporterDeployedByteCode,
				TeleporterDeployerAddress:    teleporterDeployerAddress,
				NodeCount:                    2,
				RequirePrimaryNetworkSigners: true,
			},
		},
		4,
		0,
	)
	teleporterInfo = teleporterTestUtils.NewTeleporterTestInfo(localNetworkInstance.GetAllL1Infos())

	// Only need to deploy Teleporter on the C-Chain since it is included in the genesis of the subnet chains.
	_, fundedKey := localNetworkInstance.GetFundedAccountInfo()
	teleporterInfo.DeployTeleporterMessenger(
		networkStartCtx,
		localNetworkInstance.GetPrimaryNetworkInfo(),
		teleporterDeployerTransaction,
		teleporterDeployerAddress,
		teleporterContractAddress,
		fundedKey,
	)

	// Deploy the Teleporter registry contracts to all subnets and the C-Chain.
	for _, subnet := range localNetworkInstance.GetAllL1Infos() {
		teleporterInfo.SetTeleporter(teleporterContractAddress, subnet)
		teleporterInfo.InitializeBlockchainID(subnet, fundedKey)
		teleporterInfo.DeployTeleporterRegistry(subnet, fundedKey)
	}

	// Convert the subnets to sovereign L1s
	for _, subnet := range localNetworkInstance.GetL1Infos() {
		localNetworkInstance.ConvertSubnet(
			networkStartCtx,
			subnet,
			teleporterTestUtils.PoAValidatorManager,
			[]uint64{units.Schmeckle, units.Schmeckle},
			fundedKey,
			false)
	}

	// Restart the network to attempt to refresh TLS connections
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(60*len(localNetworkInstance.Nodes))*time.Second)
	defer cancel()

	logger := logging.NewLogger("tmpnet")
	err = localNetworkInstance.Restart(ctx, logger)
	Expect(err).Should(BeNil())

	decider = exec.CommandContext(ctx, "./tests/cmd/decider/decider")
	decider.Start()
	go func() {
		err := decider.Wait()
		// Context cancellation is the only expected way for the process to exit
		// otherwise log an error but don't panic to allow for easier cleanup
		if !errors.Is(ctx.Err(), context.Canceled) {
			log.Error("Decider exited abnormally: ", "error", err)
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
	cancelFn()
	if decider != nil {
		decider = nil
	}
	if localNetworkInstance != nil {
		localNetworkInstance.TearDownNetwork()
		localNetworkInstance = nil
	}
}

var _ = ginkgo.AfterSuite(cleanup)

var _ = ginkgo.Describe("[ICM Relayer Integration Tests", func() {
	ginkgo.It("Manually Provided Message", func() {
		ManualMessage(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Basic Relay", func() {
		BasicRelay(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Shared Database", func() {
		SharedDatabaseAccess(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Allowed Addresses", func() {
		AllowedAddresses(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Batch Message", func() {
		BatchRelay(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Relay Message API", func() {
		RelayMessageAPI(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Warp API", func() {
		WarpAPIRelay(localNetworkInstance, teleporterInfo)
	})
	ginkgo.It("Signature Aggregator", func() {
		SignatureAggregatorAPI(localNetworkInstance, teleporterInfo)
	})
})
