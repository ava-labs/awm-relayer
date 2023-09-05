#!/usr/bin/env bash
# Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -o errexit
set -o nounset
set -o pipefail

# Directory above this script
RELAYER_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)
# Load the constants
source "$RELAYER_PATH"/scripts/constants.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
full_commit_hash="$(git --git-dir="$RELAYER_PATH/.git" rev-parse HEAD)"
commit_hash="${full_commit_hash::8}"

echo "Building Docker Image with tags: $relayer_dockerhub_repo:$commit_hash , $relayer_dockerhub_repo:$current_branch"
docker build -t "$relayer_dockerhub_repo:$commit_hash" \
        -t "$relayer_dockerhub_repo:$current_branch" \
        "$RELAYER_PATH" -f "$RELAYER_PATH/Dockerfile"
