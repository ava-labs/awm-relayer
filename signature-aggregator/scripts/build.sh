#!/usr/bin/env bash
# Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

go_version() {
    go version | sed -nE -e 's/[^0-9.]+([0-9.]+).+/\1/p'
}

version_lt() {
    # Return true if $1 is a lower version than than $2,
    local ver1=$1
    local ver2=$2
    # Reverse sort the versions, if the 1st item != ver1 then ver1 < ver2
    if [[ $(echo -e -n "$ver1\n$ver2\n" | sort -rV | head -n1) != "$ver1" ]]; then
        return 0
    else
        return 1
    fi
}

# Signature Aggregator root directory
SIGNATURE_AGGREGATOR_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

BASE_PATH=$(
    cd $SIGNATURE_AGGREGATOR_PATH
    cd .. && pwd
)

source $SIGNATURE_AGGREGATOR_PATH/scripts/constants.sh
source $BASE_PATH/scripts/versions.sh

go_version_minimum=$GO_VERSION

if version_lt "$(go_version)" "$go_version_minimum"; then
    echo "signature-aggregator requires Go >= $go_version_minimum, Go $(go_version) found." >&2
    exit 1
fi

if [[ $# -eq 1 ]]; then
    binary_path=$1
elif [[ $# -eq 0 ]]; then
    binary_path="$signature_aggregator_path"
else
    echo "Invalid arguments to build signature-aggregator. Requires zero (default location) or one argument to specify binary location."
    exit 1
fi

cd $SIGNATURE_AGGREGATOR_PATH
# Build AWM Relayer, which is run as a standalone process
last_git_tag=$(git describe --tags --abbrev=0 2>/dev/null) || last_git_tag="v0.0.0-dev"
echo "Building Signature Aggregator Version: $last_git_tag at $binary_path"
go build -ldflags "-X 'main.version=$last_git_tag'" -o "$binary_path" "main/"*.go
