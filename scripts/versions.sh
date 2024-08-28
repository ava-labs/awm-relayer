#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

BASE_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Pass in the full name of the dependency.
# Parses go.mod for a matching entry and extracts the version number.
function getDepVersion() {
    grep -m1 "^\s*$1" $BASE_PATH/go.mod | cut -d ' ' -f2
}

function extract_commit() {
  local version=$1
  if [[ $version == *-* ]]; then
      # Extract the substring after the last '-'
      version=${version##*-}
  fi
  echo "$version"
}

# This needs to be exported to be picked up by the dockerfile.
export GO_VERSION=${GO_VERSION:-$(getDepVersion go)}

# Don't export them as they're used in the context of other calls
AVALANCHEGO_VERSION=${AVALANCHEGO_VERSION:-$(extract_commit "$(getDepVersion github.com/ava-labs/avalanchego)")}
GINKGO_VERSION=${GINKGO_VERSION:-$(extract_commit "$(getDepVersion github.com/onsi/ginkgo/v2)")}
SUBNET_EVM_VERSION=${SUBNET_EVM_VERSION:-$(extract_commit "$(getDepVersion github.com/ava-labs/subnet-evm)")}

# Set golangci-lint version
GOLANGCI_LINT_VERSION=${GOLANGCI_LINT_VERSION:-'v1.60'}

# Set buf version
BUF_VERSION=${BUF_VERSION:-'v1.37.0'}
