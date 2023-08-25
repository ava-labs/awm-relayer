#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

go_version_minimum="1.18.1"

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

if version_lt "$(go_version)" "$go_version_minimum"; then
    echo "awm-relayer requires Go >= $go_version_minimum, Go $(go_version) found." >&2
    exit 1
fi

# Root directory
AWM_RELAYER_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Load the versions
source "$AWM_RELAYER_PATH"/scripts/versions.sh

if [[ $# -eq 1 ]]; then
    binary_path=$1
elif [[ $# -eq 0 ]]; then
    binary_path="build/awm-relayer"
else
    echo "Invalid arguments to build awm-relayer. Requires zero (default location) or one argument to specify binary location."
    exit 1
fi

# Build AWM Relayer, which is run as a standalone process
echo "Building AWM Relayer Version: $awm_relayer_version at $binary_path"
go build -o "$binary_path" "main/"*.go
