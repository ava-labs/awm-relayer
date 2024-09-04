#!/usr/bin/env bash
# Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

set -e errexit

# Root directory
root=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

source "$root"/scripts/versions.sh
go install -v "go.uber.org/mock/mockgen@$(getDepVersion go.uber.org/mock)"
PATH="$PATH:$(go env GOPATH)/bin" go generate ./...
