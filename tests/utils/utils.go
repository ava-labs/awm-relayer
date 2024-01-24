// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers"
	"github.com/ava-labs/teleporter/tests/interfaces"
	"github.com/ava-labs/teleporter/tests/utils"
	teleporterTestUtils "github.com/ava-labs/teleporter/tests/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

var (
	storageLocation = fmt.Sprintf("%s/.awm-relayer-storage", os.TempDir())
)

func RunRelayerExecutable(ctx context.Context, relayerConfigPath string) (*exec.Cmd, context.CancelFunc) {
	cmdOutput := make(chan string)

	// Run awm relayer binary with config path
	var relayerContext context.Context
	relayerContext, relayerCancel := context.WithCancel(ctx)
	relayerCmd := exec.CommandContext(relayerContext, "./build/awm-relayer", "--config-file", relayerConfigPath)

	// Set up a pipe to capture the command's output
	cmdStdOutReader, err := relayerCmd.StdoutPipe()
	Expect(err).Should(BeNil())
	cmdStdErrReader, err := relayerCmd.StderrPipe()
	Expect(err).Should(BeNil())

	// Start the command
	log.Info("Starting the relayer executable")
	err = relayerCmd.Start()
	Expect(err).Should(BeNil())

	// Start goroutines to read and output the command's stdout and stderr
	go func() {
		scanner := bufio.NewScanner(cmdStdOutReader)
		for scanner.Scan() {
			log.Info(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()
	go func() {
		scanner := bufio.NewScanner(cmdStdErrReader)
		for scanner.Scan() {
			log.Error(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()
	return relayerCmd, relayerCancel
}

func ReadHexTextFile(filename string) string {
	fileData, err := os.ReadFile(filename)
	Expect(err).Should(BeNil())
	return strings.TrimRight(string(fileData), "\n")
}

func CreateDefaultRelayerConfig(
	subnetAInfo interfaces.SubnetTestInfo,
	subnetBInfo interfaces.SubnetTestInfo,
	teleporterContractAddress common.Address,
	fundedAddress common.Address,
	relayerKey *ecdsa.PrivateKey,
) config.Config {
	hostA, portA, err := teleporterTestUtils.GetURIHostAndPort(subnetAInfo.NodeURIs[0])
	Expect(err).Should(BeNil())

	hostB, portB, err := teleporterTestUtils.GetURIHostAndPort(subnetBInfo.NodeURIs[0])
	Expect(err).Should(BeNil())

	log.Info(
		"Setting up relayer config",
		"hostA", hostA,
		"portA", portA,
		"blockChainA", subnetAInfo.BlockchainID.String(),
		"hostB", hostB,
		"portB", portB,
		"blockChainB", subnetBInfo.BlockchainID.String(),
		"subnetA", subnetAInfo.SubnetID.String(),
		"subnetB", subnetBInfo.SubnetID.String(),
	)

	return config.Config{
		LogLevel:            logging.Info.LowerString(),
		NetworkID:           peers.LocalNetworkID,
		PChainAPIURL:        subnetAInfo.NodeURIs[0],
		EncryptConnection:   false,
		StorageLocation:     RelayerStorageLocation(),
		ProcessMissedBlocks: false,
		SourceSubnets: []config.SourceSubnet{
			{
				SubnetID:          subnetAInfo.SubnetID.String(),
				BlockchainID:      subnetAInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostA,
				APINodePort:       portA,
				MessageContracts: map[string]config.MessageProtocolConfig{
					teleporterContractAddress.Hex(): {
						MessageFormat: config.TELEPORTER.String(),
						Settings: map[string]interface{}{
							"reward-address": fundedAddress.Hex(),
						},
					},
				},
			},
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				BlockchainID:      subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				MessageContracts: map[string]config.MessageProtocolConfig{
					teleporterContractAddress.Hex(): {
						MessageFormat: config.TELEPORTER.String(),
						Settings: map[string]interface{}{
							"reward-address": fundedAddress.Hex(),
						},
					},
				},
			},
		},
		DestinationSubnets: []config.DestinationSubnet{
			{
				SubnetID:          subnetAInfo.SubnetID.String(),
				BlockchainID:      subnetAInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostA,
				APINodePort:       portA,
				AccountPrivateKey: hex.EncodeToString(relayerKey.D.Bytes()),
			},
			{
				SubnetID:          subnetBInfo.SubnetID.String(),
				BlockchainID:      subnetBInfo.BlockchainID.String(),
				VM:                config.EVM.String(),
				EncryptConnection: false,
				APINodeHost:       hostB,
				APINodePort:       portB,
				AccountPrivateKey: hex.EncodeToString(relayerKey.D.Bytes()),
			},
		},
	}
}

func RelayerStorageLocation() string {
	return storageLocation
}

func ClearRelayerStorage() error {
	return os.RemoveAll(storageLocation)
}

func FundRelayers(
	ctx context.Context,
	subnetsInfo []interfaces.SubnetTestInfo,
	fundedKey *ecdsa.PrivateKey,
	relayerKey *ecdsa.PrivateKey,
) {
	relayerAddress := crypto.PubkeyToAddress(fundedKey.PublicKey)
	fundAmount := big.NewInt(0).Mul(big.NewInt(1e18), big.NewInt(10)) // 10eth

	for _, subnetInfo := range subnetsInfo {
		fundRelayerTx := utils.CreateNativeTransferTransaction(
			ctx, subnetInfo, fundedKey, relayerAddress, fundAmount,
		)
		utils.SendTransactionAndWaitForSuccess(ctx, subnetInfo, fundRelayerTx)
	}
}
