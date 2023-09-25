// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	testSubnetID    string = "2TGBXcnwx5PqiXWiqxAKUaNSqDguXNh1mxnp82jui68hxJSZAx"
	testChainID     string = "S4mMqUXe7vHsGiRAma6bv3CKnyaLssyAxmQ2KvFpX1KEvfFCD"
	testChainID2    string = "291etJW5EpagFY94v1JraFy8vLFYXcCnWKJ6Yz9vrjfPjCF4QL"
	testAddress     string = "0xd81545385803bCD83bd59f58Ba2d2c0562387F83"
	primarySubnetID string = "11111111111111111111111111111111LpoYY"
	testValidConfig        = Config{
		LogLevel:          "info",
		NetworkID:         1337,
		PChainAPIURL:      "http://test.avax.network",
		EncryptConnection: false,
		SourceSubnets: []SourceSubnet{
			{
				APINodeHost:       "http://test.avax.network",
				APINodePort:       0,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
				VM:                "evm",
				EncryptConnection: false,
				MessageContracts: map[string]MessageProtocolConfig{
					testAddress: {
						MessageFormat: TELEPORTER.String(),
					},
				},
			},
		},
		DestinationSubnets: []DestinationSubnet{
			{
				APINodeHost:       "http://test.avax.network",
				APINodePort:       0,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
				VM:                "evm",
				EncryptConnection: false,
				AccountPrivateKey: "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
		},
	}
	testPk1 string = "0xabc89e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8abc"
	testPk2 string = "0x12389e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8123"
)

func TestGetDestinationRPCEndpoint(t *testing.T) {
	testCases := []struct {
		name           string
		s              DestinationSubnet
		expectedResult string
	}{
		{
			name: "No encrypt connection",
			s: DestinationSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedResult: fmt.Sprintf("http://127.0.0.1:9650/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "Encrypt connection",
			s: DestinationSubnet{
				EncryptConnection: true,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedResult: fmt.Sprintf("https://127.0.0.1:9650/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "No port",
			s: DestinationSubnet{
				EncryptConnection: false,
				APINodeHost:       "api.avax.network",
				APINodePort:       0,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedResult: fmt.Sprintf("http://api.avax.network/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "Primary subnet",
			s: DestinationSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          primarySubnetID,
			},
			expectedResult: "http://127.0.0.1:9650/ext/bc/C/rpc",
		},
		{
			name: "Override with set rpc endpoint",
			s: DestinationSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
				RPCEndpoint:       "https://subnets.avax.network/mysubnet/rpc", // overrides all other settings used to construct the endpoint
			},
			expectedResult: "https://subnets.avax.network/mysubnet/rpc",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			res := testCase.s.GetNodeRPCEndpoint()
			require.Equal(t, testCase.expectedResult, res)
		})
	}
}

func TestGetSourceSubnetEndpoints(t *testing.T) {
	testCases := []struct {
		name              string
		s                 SourceSubnet
		expectedWsResult  string
		expectedRpcResult string
	}{
		{
			name: "No encrypt connection",
			s: SourceSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedWsResult:  fmt.Sprintf("ws://127.0.0.1:9650/ext/bc/%s/ws", testChainID),
			expectedRpcResult: fmt.Sprintf("http://127.0.0.1:9650/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "Encrypt connection",
			s: SourceSubnet{
				EncryptConnection: true,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedWsResult:  fmt.Sprintf("wss://127.0.0.1:9650/ext/bc/%s/ws", testChainID),
			expectedRpcResult: fmt.Sprintf("https://127.0.0.1:9650/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "No port",
			s: SourceSubnet{
				EncryptConnection: false,
				APINodeHost:       "api.avax.network",
				APINodePort:       0,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
			},
			expectedWsResult:  fmt.Sprintf("ws://api.avax.network/ext/bc/%s/ws", testChainID),
			expectedRpcResult: fmt.Sprintf("http://api.avax.network/ext/bc/%s/rpc", testChainID),
		},
		{
			name: "Primary subnet",
			s: SourceSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          primarySubnetID,
			},
			expectedWsResult:  "ws://127.0.0.1:9650/ext/bc/C/ws",
			expectedRpcResult: "http://127.0.0.1:9650/ext/bc/C/rpc",
		},
		{
			name: "Override with set endpoints",
			s: SourceSubnet{
				EncryptConnection: false,
				APINodeHost:       "127.0.0.1",
				APINodePort:       9650,
				ChainID:           testChainID,
				SubnetID:          testSubnetID,
				WSEndpoint:        "wss://subnets.avax.network/mysubnet/ws",    // overrides all other settings used to construct the endpoint
				RPCEndpoint:       "https://subnets.avax.network/mysubnet/rpc", // overrides all other settings used to construct the endpoint
			},
			expectedWsResult:  "wss://subnets.avax.network/mysubnet/ws",
			expectedRpcResult: "https://subnets.avax.network/mysubnet/rpc",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expectedWsResult, testCase.s.GetNodeWSEndpoint())
			require.Equal(t, testCase.expectedRpcResult, testCase.s.GetNodeRPCEndpoint())
		})
	}
}

func TestGetRelayerAccountInfo(t *testing.T) {
	type retStruct struct {
		pk   *ecdsa.PrivateKey
		addr common.Address
		err  error
	}

	testCases := []struct {
		name           string
		s              DestinationSubnet
		expectedResult retStruct
	}{
		{
			name: "valid",
			s: DestinationSubnet{
				AccountPrivateKey: "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
			expectedResult: retStruct{
				pk: &ecdsa.PrivateKey{
					D: big.NewInt(-5567472993773453273),
				},
				addr: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				err:  nil,
			},
		},
		{
			name: "invalid 0x prefix",
			s: DestinationSubnet{
				AccountPrivateKey: "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
			expectedResult: retStruct{
				pk: &ecdsa.PrivateKey{
					D: big.NewInt(-5567472993773453273),
				},
				addr: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				err:  ErrInvalidPrivateKey,
			},
		},
		{
			name: "invalid private key",
			s: DestinationSubnet{
				AccountPrivateKey: "invalid56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
			},
			expectedResult: retStruct{
				pk: &ecdsa.PrivateKey{
					D: big.NewInt(-5567472993773453273),
				},
				addr: common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"),
				err:  ErrInvalidPrivateKey,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pk, addr, err := testCase.s.GetRelayerAccountInfo()
			require.Equal(t, testCase.expectedResult.err, err)
			if err == nil {
				require.Equal(t, testCase.expectedResult.pk.D.Int64(), pk.D.Int64())
				require.Equal(t, testCase.expectedResult.addr, addr)
			}
		})
	}
}

// GetRelayerAccountPrivateKey tests. Individual cases must be run in their own functions
// because they modify the environment variables.
type getRelayerAccountPrivateKeyTestCase struct {
	baseConfig          Config
	configModifier      func(Config) Config
	flags               []string
	envSetter           func()
	expectedOverwritten bool
	resultVerifier      func(Config) bool
}

// Sets up the config file temporary environment and runs the test case.
func runGetRelayerAccountPrivateKeyTest(t *testing.T, testCase getRelayerAccountPrivateKeyTestCase) {
	root := t.TempDir()

	cfg := testCase.configModifier(testCase.baseConfig)
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)

	configFile := setupConfigJSON(t, root, string(cfgBytes))

	flags := append([]string{"--config-file", configFile}, testCase.flags...)
	testCase.envSetter()

	fs := BuildFlagSet()
	v, err := BuildViper(fs, flags)
	require.NoError(t, err)
	parsedCfg, optionOverwritten, err := BuildConfig(v)
	require.NoError(t, err)
	require.Equal(t, optionOverwritten, testCase.expectedOverwritten)

	require.True(t, testCase.resultVerifier(parsedCfg))
}

func TestGetRelayerAccountPrivateKey_set_pk_in_config(t *testing.T) {
	testCase := getRelayerAccountPrivateKeyTestCase{
		baseConfig:          testValidConfig,
		configModifier:      func(c Config) Config { return c },
		envSetter:           func() {},
		expectedOverwritten: false,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have the default private key
			for i, subnet := range c.DestinationSubnets {
				if subnet.AccountPrivateKey != utils.SanitizeHexString(testValidConfig.DestinationSubnets[i].AccountPrivateKey) {
					fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testValidConfig.DestinationSubnets[i].AccountPrivateKey), subnet.AccountPrivateKey)
					return false
				}
			}
			return true
		},
	}
	runGetRelayerAccountPrivateKeyTest(t, testCase)
}

func TestGetRelayerAccountPrivateKey_set_pk_with_subnet_env(t *testing.T) {
	testCase := getRelayerAccountPrivateKeyTestCase{
		baseConfig: testValidConfig,
		configModifier: func(c Config) Config {
			// Add a second destination subnet. This PK should NOT be overwritten
			newSubnet := c.DestinationSubnets[0]
			newSubnet.ChainID = testChainID2
			newSubnet.AccountPrivateKey = testPk1
			c.DestinationSubnets = append(c.DestinationSubnets, newSubnet)
			return c
		},
		envSetter: func() {
			// Overwrite the PK for the first subnet using an env var
			varName := fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, testValidConfig.DestinationSubnets[0].ChainID)
			t.Setenv(varName, testPk2)
		},
		expectedOverwritten: true,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have testPk1
			if c.DestinationSubnets[0].AccountPrivateKey != utils.SanitizeHexString(testPk2) {
				fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk2), c.DestinationSubnets[0].AccountPrivateKey)
				return false
			}
			if c.DestinationSubnets[1].AccountPrivateKey != utils.SanitizeHexString(testPk1) {
				fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk1), c.DestinationSubnets[1].AccountPrivateKey)
				return false
			}
			return true
		},
	}
	runGetRelayerAccountPrivateKeyTest(t, testCase)
}
func TestGetRelayerAccountPrivateKey_set_pk_with_global_env(t *testing.T) {
	testCase := getRelayerAccountPrivateKeyTestCase{
		baseConfig: testValidConfig,
		configModifier: func(c Config) Config {
			// Add a second destination subnet. This PK SHOULD be overwritten
			newSubnet := c.DestinationSubnets[0]
			newSubnet.ChainID = testChainID2
			newSubnet.AccountPrivateKey = testPk1
			c.DestinationSubnets = append(c.DestinationSubnets, newSubnet)
			return c
		},
		envSetter: func() {
			// Overwrite the PK for the first subnet using an env var
			t.Setenv(accountPrivateKeyEnvVarName, testPk2)
		},
		expectedOverwritten: true,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have testPk2
			for _, subnet := range c.DestinationSubnets {
				if subnet.AccountPrivateKey != utils.SanitizeHexString(testPk2) {
					fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk2), subnet.AccountPrivateKey)
					return false
				}
			}
			return true
		},
	}
	runGetRelayerAccountPrivateKeyTest(t, testCase)
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := filepath.Join(rootPath, "config.json")
	require.NoError(t, os.WriteFile(configFilePath, []byte(value), 0o600))
	return configFilePath
}

func TestGetRelayerAccountInfoSkipChainConfigCheckCompatible(t *testing.T) {
	accountPrivateKey := "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
	expectedAddress := "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"

	info := DestinationSubnet{
		AccountPrivateKey: accountPrivateKey,
	}
	_, address, err := info.GetRelayerAccountInfo()

	require.NoError(t, err)
	require.Equal(t, expectedAddress, address.String())
}
