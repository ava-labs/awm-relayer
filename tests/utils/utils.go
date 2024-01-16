// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"

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
