#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

# Use lower_case variables in the scripts and UPPER_CASE variables for override
# Use the constants.sh for env overrides

RELAYER_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd ) # Directory above this script

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
