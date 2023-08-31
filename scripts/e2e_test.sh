#!/usr/bin/env bash
set -e

AWM_RELAYER_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$AWM_RELAYER_PATH"/scripts/constants.sh

source "$AWM_RELAYER_PATH"/scripts/versions.sh

BASEDIR=/tmp/e2e-test AVALANCHEGO_BUILD_PATH=/tmp/e2e-test/avalanchego ./scripts/install_avalanchego_release.sh
./scripts/build.sh /tmp/e2e-test/avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
AVALANCHEGO_BUILD_PATH=/tmp/e2e-test/avalanchego DATA_DIR=/tmp/e2e-test/data ./scripts/run_ginkgo.sh
DATA_DIR=/tmp/e2e-test/data

# Build ginkgo
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}

ginkgo build ./tests/e2e

# Run the tests
./tests/e2e/e2e.test \
  --ginkgo.vv \
  --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""}