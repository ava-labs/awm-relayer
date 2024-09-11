# Avalanche ICM Off-chain Services

This repository contains off-chain services that help support Avalanche Interchain Messaging (ICM).

Currently implemented applications are 

1. [AWM Relayer](relayer/README.md)
    - Full-service cross-chain message delivery application that is configurable to listen to specific source and destination chain pairs and relay messages according to its configured rules.
2. [Signature Aggregator](signature-aggregator/README.md)
    - Lightweight API that requests and aggregates signatures from validators for any ICM message, and returns a valid signed message that the user can then self-deliver to the intended destination chain.

## Updating dependencies and E2E testing

Applications in this repository depend on the following upstream repositories, both directly in terms of code imports defined in the `go.mod` file as well as indirectly for E2E tests where binary versions are used to spin up the test network via `tmpnet`:

1.  [avalanchego](https://github.com/ava-labs/avalanchego/)
2.  [coreth](https://github.com/ava-labs/coreth) (indirectly)
3.  [subnet-evm](https://github.com/ava-labs/subnet-evm)

> [!NOTE]
> We require any commits referenced in our `main` branch to be present in the default branches of the repositories above, but during active development it might be useful to work against changes in progress that are still on feature branches.

When developing such features that require updates to one or more of the above, care must be taken to understand where the relevant code comes from. The binaries of applications built in this repo are built against versions referenced in the `go.mod` file. The E2E tests run against a simulated network running locally that is started by calling a separately compiled `avalanchego` binary as well as its plugins. These are compiled based on the values of `AVALANCHEGO_VERSION` in the local checkout of `subnet-evm` when running the tests locally and directly in this repository's `./scripts/versions.sh` when running E2E tests remotely through GitHub actions. 

`avalanchego` and `coreth` have a direct circular dependency and this repository is only indirectly dependent on `coreth` but directly dependent on `avalanchego`. Therefore if any updates are required from the `coreth` side, a corresponding `avalanchego` commit referencing those changes is required. On the other hand `subnet-evm` just depends directly on `avalanchego`.

### Example dependency update flow

The most complicated example case that can arise above is that a feature depends on a new change in `coreth`. And the steps below outline the necessary commits:

1. If an `avalanchego` commit referencing this change in its `go.mod` file doesn't exist yet then it needs to be added.
2. Add a commit in `subnet-evm` that references the `avalanchego` commit from above in both its `go.mod` file as well as its `scripts/versions.sh` file. 
3. Create a new commit in this repository referencing `avalanchego` and `subnet-evm` directly and `coreth` indirectly as well as update references in the `scripts/version.sh` file for both `AVALANCHEGO_VERSION` and `SUBNET_EVM_VERSION`.

Publishing all of the commits mentioned above to GitHub branches will enable running E2E tests through the CI.

Running the tests locally doesn't require publishing the `subnet-evm` commit since `./scripts/e2e_test.sh` takes a flag specifying local checkout of `subnet-evm` repository.

> [!NOTE]
> Locally running E2E tests using local checkout of `subnet-evm` will install `avalanchego` version specified by the `AVALANCHEGO_VERSION` in that working tree's `./scripts/versions.sh`.

> [!TIP]
> Using the local checkout it's possible to run tests against a `tmpnet` consisting of nodes using a different version of `avalanchego` than the application being tested which might be helpful when troubleshooting.

## Releases
GoReleaser is used to build the binaries of the services and also Docker images with those binaries. The monorepo feature of GoReleaser Pro is used to automate the release flow in response to tags like `signature-aggregator/v0.0.0`. The release actions in .github/workflows automate this, but the release build can also be run locally. Be sure to install the "pro" distribution of the command line utility, so that it can parse the `monorepo` key. For example:

```bash
$ goreleaser release --single-target --clean --snapshot --config signature-aggregator/.goreleaser.yml
...
    • creating                                       archive=dist/linux_amd64/signature-aggregator_0.1.0-rc0-SNAPSHOT-3c2ae78_linux_amd64.tar.gz
  • docker images
    • building docker image                          image=avaplatform/signature-aggregator:v0.1.0-rc0-amd64
...
$ docker run avaplatform/signature-aggregator:v0.1.0-rc0-amd64
```
