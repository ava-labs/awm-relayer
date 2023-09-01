#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -e

AWM_RELAYER_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

source "$AWM_RELAYER_PATH"/scripts/constants.sh

source "$AWM_RELAYER_PATH"/scripts/versions.sh

# Build ginkgo
# to install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}

ACK_GINKGO_RC=true ginkgo build ./tests/e2e

# Run the tests
echo "Running e2e tests $RUN_E2E"
RUN_E2E=true ./tests/e2e/e2e.test \
  --ginkgo.vv \
  --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""}

  echo "e2e tests passed"