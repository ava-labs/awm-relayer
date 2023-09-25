# awm-relayer

Standalone relayer for cross-chain Avalanche Warp Message delivery.

## Usage

### Building

Build the relayer by running the included build script:

```bash
./scripts/build.sh
```

Build a Docker image by running the included build script:
```
./scripts/build-local-image.sh
```

### Running

The relayer binary accepts a path to a JSON configuration file as the sole argument. Command line configuration arguments are not currently supported.

```bash
./build/awm-relayer --config-file path-to-config
```

## Architecture

**Note:** The relayer in its current state supports Teleporter messages between `subnet-evm` instances. A handful of abstractions have been added to make the relayer extensible to other Warp message formats and VM types, but this work is ongoing.

### Components

The relayer consists of the following components:

- At the global level:
    - *P2P App Network*: issues signature `AppRequests`
    - *P-Chain client*: gets the validators for a subnet
- Per Source subnet
    - *Subscriber*: listens for logs pertaining to cross-chain message transactions
- Per Destination subnet
    - *Destination RPC client*: broadcasts transactions to the destination

### Data flow

<div align="center">
  <img src="resources/relayer-diagram.png?raw=true">
</div>

## Testing

---

### Unit tests

Unit tests can be ran locally by running the command in root of the project:

```bash
./scripts/test.sh
```

### E2E tests

E2E tests are ran as part of CI, but can also be ran locally. To run the E2E tests locally, you'll need to do the following:
- Install Gingko following the intructions [here](https://onsi.github.io/ginkgo/#installing-ginkgo)
- Clone `subnet-evm` from [Github](https://github.com/ava-labs/subnet-evm) and checkout the correct version as specified in `go.mod` or `scripts/versions.sh`

Next, set up the `avalanchego` build path and `subnet-evm` binary, making sure to install everything in a writeable location (here we use `~/tmp`):

```bash
cd subnet-evm
BASEDIR=~/tmp/e2e-test AVALANCHEGO_BUILD_PATH=~/tmp/e2e-test/avalanchego ./scripts/install_avalanchego_release.sh
./scripts/build.sh ~/tmp/e2e-test/avalanchego/plugins/srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy
```

Then, in the root of the `awm-relayer` project, run:

```bash
AVALANCHEGO_BUILD_PATH=~/tmp/e2e-test/avalanchego DATA_DIR=~/tmp/e2e-test/data ./scripts/e2e_test.sh
```

Note that any additional E2E tests that run VMs other than `subnet-evm` will need to install and setup the VM binary in the same way.
### Generate Mocks

We use [gomock](https://pkg.go.dev/go.uber.org/mock/gomock) to generate mocks for testing. To generate mocks, run the following command at the root of the project:

```bash
go generate ./...
```
