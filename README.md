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

### Unit tests

Unit tests can be ran locally by running the command in root of the project:

```bash
./scripts/test.sh
```

### E2E tests

E2E tests are ran as part of CI, but can also be ran locally with the `--local` flag. To run the E2E tests locally, you'll need to install Gingko following the intructions [here](https://onsi.github.io/ginkgo/#installing-ginkgo)

Next, provide the path to the `subnet-evm` repository and the path to a writeable data directory (here we use the `~/subnet-evm` and `~/tmp/e2e-test`) to use for the tests:
```bash
./scripts/e2e_test.sh --local --subnet-evm ~/subnet-evm --data-dir ~/tmp/e2e-test
```
### Generate Mocks

We use [gomock](https://pkg.go.dev/go.uber.org/mock/gomock) to generate mocks for testing. To generate mocks, run the following command at the root of the project:

```bash
go generate ./...
```
