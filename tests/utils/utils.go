// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"os"
	"os/exec"

	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

func RunRelayerExecutable(ctx context.Context, relayerConfigPath string) (*exec.Cmd, context.CancelFunc) {
	cmdOutput := make(chan string)

	// Run awm relayer binary with config path
	var relayerContext context.Context
	relayerContext, relayerCancel := context.WithCancel(ctx)
	relayerCmd := exec.CommandContext(relayerContext, "./build/awm-relayer", "--config-file", relayerConfigPath)

	// Set up a pipe to capture the command's output
	cmdReader, _ := relayerCmd.StdoutPipe()

	// Start the command
	log.Info("Starting the relayer executable")
	err := relayerCmd.Start()
	Expect(err).Should(BeNil())

	// Start a goroutine to read and output the command's stdout
	go func() {
		scanner := bufio.NewScanner(cmdReader)
		for scanner.Scan() {
			log.Info(scanner.Text())
		}
		cmdOutput <- "Command execution finished"
	}()
	return relayerCmd, relayerCancel
}

func ReadHexTextFile(filename string) string {
	fileData, err := os.ReadFile(filename)
	Expect(err).Should(BeNil())
	return string(fileData)
}
