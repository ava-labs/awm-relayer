#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

RELAYER_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Pass in the full name of the dependency.
# Parses go.mod for a matching entry and extracts the version number.
function getDepVersion() {
    grep -m1 "^\s*$1" $RELAYER_PATH/go.mod | cut -d ' ' -f2
}

# This needs to be exported to be picked up by the dockerfile.
export GO_VERSION=${GO_VERSION:-$(getDepVersion go)}

# Don't export them as they're used in the context of other calls
# TODO: undo this hack once go.mod is referring to a tag rather than a commit
#AVALANCHEGO_VERSION=${AVALANCHEGO_VERSION:-$(getDepVersion github.com/ava-labs/avalanchego)}
AVALANCHEGO_VERSION=${AVALANCHEGO_VERSION:-376688b4912bf1b3c988167cfabf4ebedc27dc16}
GINKGO_VERSION=${GINKGO_VERSION:-$(getDepVersion github.com/onsi/ginkgo/v2)}

# TODO: undo this hack once go.mod is referring to a tag rather than a commit
#SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-$(getDepVersion github.com/ava-labs/subnet-evm)}
SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-3ceec5c96a5f83b09d06767fae83e89f79c5c2c6}

# Set golangci-lint version
GOLANGCI_LINT_VERSION=${GOLANGCI_LINT_VERSION:-'v1.55'}
