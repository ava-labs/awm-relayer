# AWM Relayer

Reference relayer implementation for cross-chain Avalanche Warp Message delivery.

AWM Relayer listens for Warp message events on a set of source blockchains, and constructs transactions to relay the Warp message to the intended destination blockchain. The relayer does so by querying the source blockchain validator nodes for their BLS signatures on the Warp message, combining the individual BLS signatures into a single aggregate BLS signature, and packaging the aggregate BLS signature into a transaction according to the destination blockchain VM Warp message verification rules.

## Installation

### Dev Container & Codespace

To get started easily, we provide a Dev Container specification, that can be used using GitHub Codespace or locally using Docker and VS Code. [Dev Containers](https://code.visualstudio.com/docs/devcontainers/containers) are a concept that utilizes containerization to create consistent and isolated development environment. You can run them directly on Github by clicking **Code**, switching to the **Codespaces** tab and clicking **Create codespace on main**. Alternatively, you can run them locally with the extensions for [VS Code](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) or other code editors.

### Download Prebuilt Binaries

Prebuilt binaries are available for download from the [releases page](https://github.com/ava-labs/awm-relayer/releases).

The following commands demonstrate how to download and install the v0.2.13 release of the relayer on MacOS. The exact commands will vary by platform.

```bash
# Download the release tarball and checksums
curl -w '%{http_code}' -sL -o ~/Downloads/awm-relayer_0.2.13_darwin_arm64.tar.gz https://github.com/ava-labs/awm-relayer/releases/download/v0.2.13/awm-relayer_0.2.13_darwin_arm64.tar.gz
curl -w '%{http_code}' -sL -o ~/Downloads/awm-relayer_0.2.13_checksums.txt https://github.com/ava-labs/awm-relayer/releases/download/v0.2.13/awm-relayer_0.2.13_checksums.txt

# (Optional) Verify the checksums
cd ~/Downloads
# Confirm that the following two commands output the same checksum
grep "awm-relayer_0.2.13_darwin_arm64.tar.gz" "awm-relayer_0.2.13_checksums.txt" 2>/dev/null
shasum -a 256 "awm-relayer_0.2.13_darwin_arm64.tar.gz" 2>/dev/null

# Extract the tarball and install the relayer binary
tar -xzf awm-relayer_0.2.13_darwin_arm64.tar.gz
sudo install awm-relayer /usr/local/bin
```

_Note:_ If downloading the binaries through a browser on MacOS, the browser may mark the binary as quarantined since it has not been verified through the App Store. To remove the quarantine, run the following command:

```bash
xattr -d com.apple.quarantine /usr/local/bin/awm-relayer
```

### Download Docker Image

The published Docker image can be pulled from `avaplatform/awm-relayer:latest` on dockerhub.

### Build from Source

See the [Building](#building) section for instructions on how to build the relayer from source.

## Requirements

### System Requirements

- Ubuntu 22.04 or later
  - Tested on x86_64/amd64 architecture.
- MacOS 14.3 or later
  - Tested on arm64 architecture (Apple silicon).

### API Requirements

- AWM Relayer requires access to Avalanche API nodes for the P-Chain as well as any connected Subnets. The API nodes must have the following methods enabled:
  - Each Subnet API node must have enabled:
    - eth API (RPC and WS)
  - The P-Chain API node must have enabled:
    - platform.getHeight
    - platform.validatedBy
    - platform.getValidatorsAt OR platform.getCurrentValidators
  - The Info API node must have enabled:
    - info.peers
    - info.getNetworkID
  - If the Info API node is also a Subnet validator, it must have enabled:
    - info.getNodeID
    - info.getNodeIP

The Fuji and Mainnet [public API nodes](https://docs.avax.network/tooling/rpc-providers) provided by Avalanche have these methods enabled, and are suitable for use with the relayer.

### Peer-to-Peer Connections

- By default, the AWM relayer implementation gathers BLS signatures from the validators of the source Subnet via peer-to-peer `AppRequest` messages. Validator nodes need to be configured to accept incoming peer connections. Otherwise, the relayer will fail to gather Warp message signatures. For example, networking rules may need to be adjusted to allow traffic on the default AvalancheGo P2P port (9651), or the public IP may need to be manually set in the [node configuration](https://docs.avax.network/nodes/configure/avalanchego-config-flags#public-ip).
- If configured to use the Warp API (see `warp-api-endpoint` in [Configuration](#configuration)) then aggregate signatures are fetched via a single RPC request, rather than `AppRequests` to individual validators. Note that the Warp API is disabled on the public API.

### Private Key Management

- Each configured destination blockchain requires a private key to sign transactions. This key can be provided as a hex-encoded string in the configuration (see `account-private-key` in [Configuration](#configuration)) or environment variable, or stored in KMS and used to sign transactions remotely (see `kms-key-id` and `kms-aws-region` in [Configuration](#configuration)). 
- **Each private key used by the relayer should not be used to sign transactions outside of the relayer**, as this may cause the relayer to fail to sign transactions due to nonce mismatches.

## Usage

### Options

The relayer binary accepts the following command line options. Other configuration options are not supported via the command line and must be provided via the configuration JSON file or environment variable.

```bash
awm-relayer --config-file path-to-config                Specifies the relayer config file and begin relaying messages.
awm-relayer --version                                   Display awm-relayer version and exit.
awm-relayer --help                                      Display awm-relayer usage and exit.
```

### Initialize the repository

- Get all submodules: `git submodule update --init --recursive`

### Building

Before building, be sure to install Go, which is required even if you're just building the Docker image. You'll also need to install [buf](github.com/bufbuild/buf/).

Build the relayer by running the script:

```bash
./scripts/build.sh
```

Build a Docker image by running the script:

```bash
./scripts/build_local_image.sh
```

### Configuration

The relayer is configured via a JSON file, the path to which is passed in via the `--config-file` command line argument. Top level configuration options are also able to be set via environment variable. To get the environment variable corresponding to a key, upper case the key and change the delimiter from "-" to "_". For example, `LOG_LEVEL` sets the `"log-level"` JSON key. The following configuration options are available:

`"log-level": "verbo" | "debug" | "info" | "warn" | "error" | "fatal" | "panic"`

- The log level for the relayer. Defaults to `info`.

`"p-chain-api": APIConfig`

- The configuration for the Avalanche P-Chain API node. The `PChainAPI` object has the following configuration:

  `"base-url": string`

  - The URL of the Avalanche P-Chain API node to which the relayer will connect. This API node needs to have the following methods enabled:
    - platform.getHeight
    - platform.validatedBy
    - platform.getValidatorsAt OR platform.getCurrentValidators

  `"query-parameters": map[string]string`

  - Additional query parameters to include in the API requests.

  `"http-headers": map[string]string`

  - Additional HTTP headers to include in the API requests.

`"info-api": APIConfig`

- The configuration for the Avalanche Info API node. The `InfoAPI` object has the following configuration:

  `"base-url": string`

  - The URL of the Avalanche Info API node to which the relayer will connect. This API node needs to have the following methods enabled:

    - info.peers
    - info.getNetworkID

  - Additionally, if the Info API node is also a validator, it must have enabled:
    - info.getNodeID
    - info.getNodeIP

  `"query-parameters": map[string]string`

  - Additional query parameters to include in the API requests.

  `"http-headers": map[string]string`

  - Additional HTTP headers to include in the API requests.

`"storage-location": string`

- The path to the directory in which the relayer will store its state. Defaults to `./awm-relayer-storage`.

`"redis-url": string`

- The URL of the Redis server to use to manage state. This URL should specify the user, password, host, port, DB index, and protocol version. For example, `"redis://user:password@localhost:6379/0?protocol=3"`. Overrides `storage-location` if provided.

`"process-missed-blocks": boolean`

- Whether or not to process missed blocks after restarting. Defaults to `true`. If set to false, the relayer will start processing blocks from the chain head.

`"api-port"`: unsigned integer

- The port on which the relayer will listen for API requests. Defaults to `8080`.

`"metrics-port"`: unsigned integer

- The port on which the relayer will expose Prometheus metrics. Defaults to `9090`.

`"db-write-interval-seconds": unsigned integer`

- The interval at which the relayer will write to the database. Defaults to `10`.

`"manual-warp-messages": []ManualWarpMessage`

- The list of Warp messages to relay on startup, independent of the catch-up mechanism or normal operation. Each `ManualWarpMessage` has the following configuration:

  `"unsigned-message-bytes": string`

  - The hex-encoded bytes of the unsigned Warp message to relay.

  `"source-blockchain-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded blockchain ID of the source blockchain.

  `"destination-blockchain-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded blockchain ID of the destination blockchain.

  `"source-address": string`

  - The address of the source account that sent the Warp message.

  `"destination-address": string`

  - The address of the destination account that will receive the Warp message.

`"source-blockchains": []SourceBlockchains`

- The list of source blockchains to support. Each `SourceBlockchain` has the following configuration:

  `"subnet-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded Subnet ID.

  `"blockchain-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded blockchain ID.

  `"vm": string`

  - The VM type of the source blockchain.

  `"rpc-endpoint": APIConfig`

  - The RPC endpoint configuration of the source blockchain's API node.

  `"ws-endpoint": APIConfig`

  - The WebSocket endpoint configuration of the source blockchain's API node.

  `"message-contracts": map[string]MessageProtocolConfig`

  - Map of contract addresses to the config options of the protocol at that address. Each `MessageProtocolConfig` consists of a unique `message-format` name, and the raw JSON `settings`.

  `"supported-destinations": []SupportedDestination`

  - List of destinations that the source blockchain supports. Each `SupportedDestination` consists of a cb58-encoded destination blockchain ID (`"blockchain-id"`), and a list of hex-encoded addresses (`"addresses"`) on that destination blockchain that the relayer supports delivering Warp messages to. The destination address is defined by the message protocol. For example, it could be the address called from the message protocol contract. If no supported addresses are provided, all addresses are allowed on that blockchain. If `supported-destinations` is empty, then all destination blockchains (and therefore all addresses on those destination blockchains) are supported.

  `"process-historical-blocks-from-height": unsigned integer`

  - The block height at which to back-process transactions from the source blockchain. If the database already contains a later block height for the source blockchain, then that will be used instead. Must be non-zero. Will only be used if `process-missed-blocks` is set to `true`.

  `"allowed-origin-sender-addresses": []string`

  - List of addresses on this source blockchain to relay Warp messages from. The sending address is defined by the message protocol. For example, it could be defined as the EOA that initiates the transaction, or the address that calls the message protocol contract. If empty, then all addresses are allowed.

  `"warp-api-endpoint": APIConfig`

  - The RPC endpoint configuration for the Warp API, which is used to fetch Warp aggregate signatures. If omitted, then signatures are fetched via AppRequest instead.

`"destination-blockchains": []DestinationBlockchains`

- The list of destination blockchains to support. Each `DestinationBlockchain` has the following configuration:

  `"subnet-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded Subnet ID.

  `"blockchain-id": string`

  - cb58-encoded or "0x" prefixed hex-encoded blockchain ID.

  `"vm": string`

  - The VM type of the source blockchain.

  `"rpc-endpoint": APIConfig`

  - The RPC endpoint configuration of the destination blockchains's API node.

  `"account-private-key": string`

  - The hex-encoded private key to use for signing transactions on the destination blockchain. May be provided by the environment variable `ACCOUNT_PRIVATE_KEY`. Each `destination-subnet` may use a separate private key by appending the cb58 encoded blockchain ID to the private key environment variable name, for example `ACCOUNT_PRIVATE_KEY_11111111111111111111111111111111LpoYY`
  - Please note that the private key should be exclusive to the relayer, see [Private Key Management](#private-key-management).

  `"kms-key-id": string`

  - The ID of the KMS key to use for signing transactions on the destination blockchain. Only one of `account-private-key` or `kms-key-id` should be provided. If `kms-key-id` is provided, then `kms-aws-region` is required.
  - Please note that the private key in KMS should be exclusive to the relayer, see [Private Key Management](#private-key-management).

  `"kms-aws-region": string`

  - The AWS region in which the KMS key is located. Required if `kms-key-id` is provided.

`"decider-host": string`

`"decider-port": unsigned integer`

- The network location of a service implementing the gRPC service defined by `proto/decider`, which will be queried for each message to determine whether that message should be relayed. If a port is specified but a host is not, the host is assumed to be `localhost`.

## Architecture

### Components

The relayer consists of the following components:

- At the global level:
  - P2P app network: issues signature `AppRequests`
  - P-Chain client: gets the validators for a Subnet
  - Relayer database: stores latest processed block for each Application Relayer
    - Currently supports Redis and local JSON file storage
- Per Source Blockchain
  - Subscriber: listens for logs pertaining to cross-chain message transactions
  - Source RPC client: queries for missed blocks on startup
- Per Destination Blockchain
  - Destination RPC client: broadcasts transactions to the destination
- Application Relayers
  - Relay messages from a specific source blockchain and source address to a specific destination blockchain and destination address

### Data Flow

<div align="center">
  <img src="resources/relayer-diagram.png?raw=true"></img>
</div>

### API

#### `/relay`
- Used to manually relay a Warp message. The body of the request must contain the following JSON:
```json
{
 "blockchain-id": "<cb58-encoded or '0x' prefixed hex-encoded of blockchain ID>",
 "message-id": "<cb58-encoded or '0x' prefixed hex-encoded of Warp message ID>",
 "block-num": "<Block number that the message was sent in>"
}
```
- If successful, the endpoint will return the following JSON:
```json
{
 "transaction-hash": "<Transaction hash that includes the delivered warp message>"
}
```

#### `/relay/message`
- Used to manually relay a warp message. The body of the request must contain the following JSON:
```json
{
 "unsigned-message-bytes": "<Hex encoded byte array containing the unsigned warp message>",
 "source-address": "<Hex encoding of address that sent the warp message>"
}
```
- If successful, the endpoint will return the following JSON:
```json
{
 "transaction-hash": "<Transaction hash that includes the delivered Warp message>",
}
```

#### `/health`
- Takes no arguments. Returns a `200` status code if all Application Relayers are healthy. Returns a `503` status if any of the Application Relayers have experienced an unrecoverable error. Here is an example return body:
```json
{
  "status": "down",
  "details": {
    "relayers-all": {
      "status": "down",
      "timestamp": "2024-06-01T05:06:07.685522Z",
      "error": "<List of cb-58 encoded IDs for unhealthy relayers>"
    }
  }
}
```

## Testing

### Unit Tests

Unit tests can be ran locally by running the command in the root of the project:

```bash
./scripts/test.sh
```

If your temporary directory is not writable, the unit tests may fail with messages like `fork/exec /tmp/go-build2296620589/b247/config.test: permission denied`. To fix this, set the `TMPDIR` environment variable to something writable, for example `export TMPDIR=~/tmp`.

### E2E Tests

E2E tests are ran as part of CI, but can also be ran locally with the `--local` flag. To run the E2E tests locally, you'll need to install Gingko following the instructions [here](https://onsi.github.io/ginkgo/#installing-ginkgo)

Next, provide the path to the `subnet-evm` repository and the path to a writeable data directory (this example uses `~/subnet-evm` and `~/tmp/e2e-test`) to use for the tests:

```bash
./scripts/e2e_test.sh --local --subnet-evm ~/subnet-evm --data-dir ~/tmp/e2e-test
```

To run a specific E2E test, specify the environment variable `GINKGO_FOCUS`, which will then look for [test descriptions](./tests/e2e_test.go#L68) that match the provided input. For example, to run the `Basic Relay` test:

```bash
GINKGO_FOCUS="Basic" ./scripts/e2e_test.sh --local --subnet-evm ~/subnet-evm --data-dir ~/tmp/e2e-test
```

The E2E tests use the `TeleporterMessenger` contract deployment transaction specified in the following files:

- `tests/utils/UniversalTeleporterDeployerAddress.txt`
- `tests/utils/UniversalTeleporterDeployerTransaction.txt`
- `tests/utils/UniversalTeleporterMessagerContractAddress.txt`
  To update the version of Teleporter used by the E2E tests, update these values with the latest contract deployment information. For more information on how to deploy the Teleporter contract, see the [Teleporter documentation](https://github.com/ava-labs/teleporter/tree/main/utils/contract-deployment).

### Generate Mocks

[Gomock](https://pkg.go.dev/go.uber.org/mock/gomock) is used to generate mocks for testing. To generate mocks, run the following command at the root of the project:

```bash
go generate ./...
```
