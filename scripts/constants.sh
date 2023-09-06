#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

RELAYER_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Where AWM Relayer binary goes
relayer_path="$RELAYER_PATH/build/awm-relayer"

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Avalabs docker hub repo is avaplatform/awm-relayer.
# Here we default to the local image (awm-relayer) as to avoid unintentional pushes
# You should probably set it - export DOCKER_REPO='avaplatform/awm-relayer'
relayer_dockerhub_repo=${DOCKER_REPO:-"awm-relayer"}

# Current branch
current_branch=$(git symbolic-ref -q --short HEAD || git describe --tags --exact-match || true)

git_commit=${RELAYER_COMMIT:-$( git rev-list -1 HEAD )}

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
# While CGO_ENABLED doesn't need to be explicitly set, it produces a much more
# clear error due to the default value change in go1.20.
export CGO_ENABLED=1