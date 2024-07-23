# Signature Aggregator 

This directory contains a lightweight stand-alone API for requesting signatures for a Warp message from subnet validators. 
It is also used by the relayer for gathering signatures when configured to use AppRequest instead of the WarpSignature RPC client.

## Building

To build the application run `signature-aggregator/scripts/build.sh` which will output the binary to `signature-aggregator/build/signature-aggregator` path by default. 

## Running

To run the binary you must supply a config file via `./signature-aggregator --config-file`
Currently required configurations are a small subset of the [relayer configuration](https://github.com/ava-labs/awm-relayer?tab=readme-ov-file#configuration).

Namely:
- `LogLevel`: string
- `PChainAPI`: APIConfig
- `InfoAPI` : APIConfig
- `APIPort` : (optional) defaults to 8080

Since it's a subset you can run the application using the example `sample-relayer-config.json` via `./signature-agregator/build/signature-aggregator --config-file sample-relayer-config.json`