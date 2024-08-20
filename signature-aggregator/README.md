# Signature Aggregator

This directory contains a lightweight stand-alone API for requesting signatures for a Warp message from Subnet validators.
It is also used by `awm-relayer` for gathering signatures when configured to use AppRequest instead of the Warp signature RPC client.

## Building

To build the application run `scripts/build_signature_aggregator.sh` which will output the binary to `signature-aggregator/build/signature-aggregator` path by default.

## Running

To run the binary you must supply a config file via `./signature-aggregator --config-file`
Currently required configurations are a small subset of the [`awm-relayer` configuration](https://github.com/ava-labs/awm-relayer?tab=readme-ov-file#configuration).

Namely:
- `LogLevel`: string
- `PChainAPI`: APIConfig
- `InfoAPI` : APIConfig
- `APIPort` : (optional) defaults to 8080
- `MetricsPort`: (optional) defaults to 8081

Sample config that can be used for local testing is `signature-aggregator/sample-signature-aggregator-config.json`

## Interface

The only exposed endpoint is `/aggregate-signatures`  expecting `application/json` encoded request with the following body. Note that all the fields are optional but at least one of `message` or `justification` must be non-empty:
```json
{
    "message": "",            // (string) hex-encoded unsigned message bytes to be signed
    "justification": "",      // (string) hex-encoded bytes to supply to the validators as justification
    "signing-subnet-id": "",  // (string) hex or cb58 encoded signing subnet ID. Defaults to source blockchain's subnet from data if omitted.
    "quorum-percentage": 67  // (int) quorum percentage required to sign the message. Defaults to 67 if omitted
}
```

## Sample workflow
If you want to manually test a locally running service pointed to the Fuji testnet you can do so with the following steps.

Note that this might fail for older messages if there has been enough validator churn, and less then the threshold weight of stake of validators have seen the message when it originated. In this case try picking a more recent message.

The basic request consists of sending just the `data` field containing the hex-encoded bytes of the full unsigned Warp message that the validators would be willing to sign. Here are the steps to obtain a sample valid unsigned Warp message bytes from the Fuji network.

1. Find a valid on-chain `Receive Cross Chain Message` Transaction
   This can be done by looking at the `Teleporter` [contract tracker](https://subnets-test.avax.network/c-chain/address/0x253b2784c75e510dD0fF1da844684a1aC0aa5fcf) on the Fuji-C
2. Get the transaction receipt and logs for this transaction. This can be done through the explorer or via a curl to the API:
```bash
curl --location 'https://api.avax-test.network/ext/bc/C/rpc' \
--header 'Content-Type: application/json' \
--data '{
    "jsonrpc": "2.0",
    "method": "eth_getTransactionReceipt",
    "params": [
        "<hex_encoded_transaction>"
    ],
    "id": 1
}'
```
3. Search these logs for the `SendWarpMessage` Log event emitted from the Warp precompile address (`0x0200000000000000000000000000000000000005`)
   The topic of the message will be `0x56600c567728a800c0aa927500f831cb451df66a7af570eb4df4dfbf4674887d` which is the output of`cast keccak "SendWarpMessage(address,bytes32,bytes)"`
4. Use the data field of the log message found in step 2 and send it to the locally running service via curl.
```bash
curl --location 'http://localhost:8080/aggregate-signatures/by-raw-message' \
--header 'Content-Type: application/json' \
--data '{
    "data": "<hex encoded unsigned message bytes retrieved from the logs>",
}'
```
