#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

VERBOSE=
HELP=
while [ $# -gt 0 ]; do
    case "$1" in
        -v | --verbose) VERBOSE=-test.v ;;
        -h | --help) HELP=true ;;
    esac
    shift
done

if [ "$HELP" = true ]; then
    echo "Usage: ./scripts/test.sh [OPTIONS]"
    echo "Run unit tests for AWM Relayer."
    echo ""
    echo "Options:"
    echo "  -v, --verbose                     Run the test with verbose output"
    echo "  -h, --help                        Print this help message"
    exit 0
fi

# Directory above this script
RELAYER_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)
source "$RELAYER_PATH"/scripts/constants.sh
go test -tags testing $VERBOSE ./...