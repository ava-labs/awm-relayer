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

### Generate Mocks

We use [gomock](https://pkg.go.dev/go.uber.org/mock/gomock) to generate mocks for testing. To generate mocks, run the following command at the root of the project:

```bash
go generate ./...
```