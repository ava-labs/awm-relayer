#!/usr/bin/env bash
set -e

AWM_RELAYER_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$AWM_RELAYER_PATH"/scripts/constants.sh

source "$AWM_RELAYER_PATH"/scripts/versions.sh

RUN_E2E=true

# Build ginkgo
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}

ACK_GINKGO_RC=true ginkgo build ./tests/e2e

# Run the tests
./tests/e2e/e2e.test \
  --ginkgo.vv \
  --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""}