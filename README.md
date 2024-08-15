# Avalanche ICM Off-chain Services

This repository contains off-chain services that help support Avalanche Interchain Messaging (ICM).

Currently implemented applications are 

1. [AWM Relayer](relayer/README.md)
    - Full-service cross-chain message delivery application that is configurable to listen to specific source and destination chain pairs and relay messages according to its configured rules.
2. [Signature Aggregator](signature-aggregator/README.md)
    - Lightweight API that requests and aggregates signatures from validators for any ICM message, and returns a valid signed message that the user can then self-deliver to the intended destination chain.
