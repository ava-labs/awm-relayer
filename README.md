# awm-relayer

Standalone relayer for cross-chain Avalanche Warp Message delivery.

## Usage
---
### Building
Build the relayer by running the included build script:
```
./scripts/build.sh
```
### Running
The relayer binary accepts a path to a JSON configuration file as the sole argument. Command line configuration arguments are not currently supported.
```
./build/awm-relayer --config-file path-to-config
```

## Architecture
---
**Note:** The relayer in its current state supports Teleporter messages between subnet-evm instances. A handful of abstractions have been added to make the relayer extensible to other Warp message formats and VM types, but this work is ongoing.
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

## v0.1.x versus v0.2.x
`v0.2.x` tracks `main` and represents the latest version of `awm-relayer`, which stays up-to-date with `avalanchego` and `subnet-evm`. However, while Avalanche Warp Messaging is still under active development, `v0.1.x` uses stable versions of `avalanchego` and `subnet-evm` to avoid breaking changes. The `awm-relayer` deployment managed by Ava Labs, along with the Fuji subnets Amplify, Bulletin, and Conduit, are compatible with the stable `v0.1.x` but **not** `v0.2.x`. Any interaction with those subnets should be done with the correct version. Interaction with local networks (such as in the integration test suite in `teleporter`) may use `v0.2.x`.