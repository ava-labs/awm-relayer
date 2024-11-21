#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -e

HELP=
LOG_LEVEL=
while [ $# -gt 0 ]; do
    case "$1" in
        -v | --verbose) LOG_LEVEL=debug ;;
        -h | --help) HELP=true ;;
    esac
    shift
done

if [ "$HELP" = true ]; then
    echo "Usage: ./scripts/e2e_test.sh [OPTIONS]"
    echo "Run E2E tests for AWM Relayer."
    echo ""
    echo "Options:"
    echo "  -v, --verbose                     Enable debug logs"
    echo "  -h, --help                        Print this help message"
    exit 0
fi

BASE_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$BASE_PATH"/scripts/constants.sh
source "$BASE_PATH"/scripts/versions.sh

BASEDIR=${BASEDIR:-"$HOME/.teleporter-deps"}

cwd=$(pwd)
# Install the avalanchego and subnet-evm binaries
rm -rf $BASEDIR/avalanchego
BASEDIR=$BASEDIR AVALANCHEGO_BUILD_PATH=$BASEDIR/avalanchego ./scripts/install_avalanchego_release.sh"
BASEDIR=$BASEDIR "${TELEPORTER_PATH}/scripts/install_subnetevm_release.sh"

cp ${BASEDIR}/subnet-evm/subnet-evm ${BASEDIR}/avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
echo "Copied ${BASEDIR}/subnet-evm/subnet-evm binary to ${BASEDIR}/avalanchego/plugins/"

export AVALANCHEGO_BUILD_PATH=$BASEDIR/avalanchego

# Build ginkgo
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}

ginkgo build ./tests/
go build -v -o tests/cmd/decider/decider ./tests/cmd/decider/

# Run the tests
echo "Running e2e tests $RUN_E2E"
RUN_E2E=true LOG_LEVEL=${LOG_LEVEL} ./tests/tests.test \
  --ginkgo.vv \
  --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""} \
  --ginkgo.focus=${GINKGO_FOCUS:-""} 

echo "e2e tests passed"
exit 0
