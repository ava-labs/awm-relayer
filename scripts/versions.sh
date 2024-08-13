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
AVALANCHEGO_VERSION=${AVALANCHEGO_VERSION:-4ef571141d434756610a7f6f4e2c0bd7efa7f602}
GINKGO_VERSION=${GINKGO_VERSION:-$(getDepVersion github.com/onsi/ginkgo/v2)}

# TODO: undo this hack once go.mod is referring to a tag rather than a commit
#SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-$(getDepVersion github.com/ava-labs/subnet-evm)}
SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-4d5aebb6decc91685ab3b700cb2f6a7b697d1e2f}

# Set golangci-lint version
GOLANGCI_LINT_VERSION=${GOLANGCI_LINT_VERSION:-'v1.55'}
