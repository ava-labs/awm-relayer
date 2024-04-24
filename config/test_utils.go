//go:build testing

package config

import "fmt"

var (
	testSubnetID      string = "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx"
	testBlockchainID  string = "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD"
	testBlockchainID2 string = "291etJW5EpagFY94v1JraFy8vLFYXcCnWKJ6Yz9vrjfPjCF4QL"
	testAddress       string = "0xd81545385803bCD83bd59f58Ba2d2c0562387F83"
	testPk1           string = "0xabc89e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8abc"
	testPk2           string = "0x12389e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8123"
)

// Valid configuration objects to be used by tests in external packages
var (
	TestValidConfig = Config{
		LogLevel: "info",
		PChainAPI: &PChainAPI{
			apiClient: apiClient{
				BaseURL: "http://test.avax.network",
			},
			// BaseURL: "http://test.avax.network",
		},
		InfoAPI: &InfoAPI{
			apiClient: apiClient{
				BaseURL: "http://test.avax.network",
			},
		},
		SourceBlockchains: []*SourceBlockchain{
			{
				RPCEndpoint:  fmt.Sprintf("http://test.avax.network/ext/bc/%s/rpc", testBlockchainID),
				WSEndpoint:   fmt.Sprintf("ws://test.avax.network/ext/bc/%s/ws", testBlockchainID),
				BlockchainID: testBlockchainID,
				SubnetID:     testSubnetID,
				VM:           "evm",
				MessageContracts: map[string]MessageProtocolConfig{
					testAddress: {
						MessageFormat: TELEPORTER.String(),
					},
				},
			},
		},
		DestinationBlockchains: []*DestinationBlockchain{
			{
				RPCEndpoint:       fmt.Sprintf("http://test.avax.network/ext/bc/%s/rpc", testBlockchainID),
				BlockchainID:      testBlockchainID,
				SubnetID:          testSubnetID,
				VM:                "evm",
				AccountPrivateKey: "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
		},
	}
	TestValidSourceBlockchainConfig = SourceBlockchain{
		RPCEndpoint:  "http://test.avax.network/ext/bc/C/rpc",
		WSEndpoint:   "ws://test.avax.network/ext/bc/C/ws",
		BlockchainID: "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
		SubnetID:     "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
		VM:           "evm",
		MessageContracts: map[string]MessageProtocolConfig{
			"0xd81545385803bCD83bd59f58Ba2d2c0562387F83": {
				MessageFormat: TELEPORTER.String(),
			},
		},
	}
	TestValidDestinationBlockchainConfig = DestinationBlockchain{
		SubnetID:          "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx",
		BlockchainID:      "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD",
		VM:                "evm",
		RPCEndpoint:       "http://test.avax.network/ext/bc/C/rpc",
		AccountPrivateKey: "1234567890123456789012345678901234567890123456789012345678901234",
	}
)
