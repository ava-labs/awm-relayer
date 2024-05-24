// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/utils"
	mock_ethclient "github.com/ava-labs/awm-relayer/vms/evm/mocks"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	awsRegion string = "us-west-2"
	kmsKey1   string = "test-kms-id1"
)

// GetRelayerAccountPrivateKey tests. Individual cases must be run in their own functions
// because they modify the environment variables.
type configMondifierEnvVarTestCase struct {
	baseConfig          Config
	configModifier      func(Config) Config
	flags               []string
	envSetter           func()
	expectedOverwritten bool
	resultVerifier      func(Config) bool
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := filepath.Join(rootPath, "config.json")
	require.NoError(t, os.WriteFile(configFilePath, []byte(value), 0o600))
	return configFilePath
}

// Sets up the config file temporary environment and runs the test case.
func runConfigModifierEnvVarTest(t *testing.T, testCase configMondifierEnvVarTestCase) {
	root := t.TempDir()

	cfg := testCase.configModifier(testCase.baseConfig)
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)

	configFile := setupConfigJSON(t, root, string(cfgBytes))

	flags := append([]string{"--config-file", configFile}, testCase.flags...)
	testCase.envSetter()

	fs := BuildFlagSet()
	if err := fs.Parse(flags); err != nil {
		panic(fmt.Errorf("couldn't parse flags: %w", err))
	}
	v, err := BuildViper(fs)
	require.NoError(t, err)
	parsedCfg, err := NewConfig(v)
	require.NoError(t, err)
	require.Equal(t, parsedCfg.HasOverwrittenOptions(), testCase.expectedOverwritten)

	require.True(t, testCase.resultVerifier(parsedCfg))
}

func TestGetRelayerAccountPrivateKey_set_pk_in_config(t *testing.T) {
	testCase := configMondifierEnvVarTestCase{
		baseConfig:          TestValidConfig,
		configModifier:      func(c Config) Config { return c },
		envSetter:           func() {},
		expectedOverwritten: false,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have the default private key
			for i, subnet := range c.DestinationBlockchains {
				if subnet.AccountPrivateKey != utils.SanitizeHexString(TestValidConfig.DestinationBlockchains[i].AccountPrivateKey) {
					fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(TestValidConfig.DestinationBlockchains[i].AccountPrivateKey), subnet.AccountPrivateKey)
					return false
				}
			}
			return true
		},
	}
	runConfigModifierEnvVarTest(t, testCase)
}

func TestGetRelayerAccountPrivateKey_set_pk_with_subnet_env(t *testing.T) {
	testCase := configMondifierEnvVarTestCase{
		baseConfig: TestValidConfig,
		configModifier: func(c Config) Config {
			// Add a second destination subnet. This PK should NOT be overwritten
			newSubnet := *c.DestinationBlockchains[0]
			newSubnet.BlockchainID = testBlockchainID2
			newSubnet.AccountPrivateKey = testPk1
			c.DestinationBlockchains = append(c.DestinationBlockchains, &newSubnet)
			return c
		},
		envSetter: func() {
			// Overwrite the PK for the first subnet using an env var
			varName := fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, TestValidConfig.DestinationBlockchains[0].BlockchainID)
			t.Setenv(varName, testPk2)
		},
		expectedOverwritten: true,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have testPk1
			if c.DestinationBlockchains[0].AccountPrivateKey != utils.SanitizeHexString(testPk2) {
				fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk2), c.DestinationBlockchains[0].AccountPrivateKey)
				return false
			}
			if c.DestinationBlockchains[1].AccountPrivateKey != utils.SanitizeHexString(testPk1) {
				fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk1), c.DestinationBlockchains[1].AccountPrivateKey)
				return false
			}
			return true
		},
	}
	runConfigModifierEnvVarTest(t, testCase)
}

func TestGetRelayerAccountPrivateKey_set_pk_with_global_env(t *testing.T) {
	testCase := configMondifierEnvVarTestCase{
		baseConfig: TestValidConfig,
		configModifier: func(c Config) Config {
			// Add a second destination subnet. This PK SHOULD be overwritten
			newSubnet := *c.DestinationBlockchains[0]
			newSubnet.BlockchainID = testBlockchainID2
			newSubnet.AccountPrivateKey = testPk1
			c.DestinationBlockchains = append(c.DestinationBlockchains, &newSubnet)
			return c
		},
		envSetter: func() {
			// Overwrite the PK for the first subnet using an env var
			t.Setenv(accountPrivateKeyEnvVarName, testPk2)
		},
		expectedOverwritten: true,
		resultVerifier: func(c Config) bool {
			// All destination subnets should have testPk2
			for _, subnet := range c.DestinationBlockchains {
				if subnet.AccountPrivateKey != utils.SanitizeHexString(testPk2) {
					fmt.Printf("expected: %s, got: %s\n", utils.SanitizeHexString(testPk2), subnet.AccountPrivateKey)
					return false
				}
			}
			return true
		},
	}
	runConfigModifierEnvVarTest(t, testCase)
}

func TestEitherKMSOrAccountPrivateKey(t *testing.T) {
	dstCfg := *TestValidConfig.DestinationBlockchains[0]
	// Zero out all fields under test
	dstCfg.AccountPrivateKey = ""
	dstCfg.KMSKeyID = ""
	dstCfg.KMSAWSRegion = ""

	testCases := []struct {
		name   string
		dstCfg func() DestinationBlockchain
		valid  bool
	}{
		{
			name: "kms supplied",
			dstCfg: func() DestinationBlockchain {
				cfg := dstCfg
				cfg.KMSKeyID = kmsKey1
				cfg.KMSAWSRegion = awsRegion
				return cfg
			},
			valid: true,
		},
		{
			name: "account private key supplied",
			dstCfg: func() DestinationBlockchain {
				cfg := dstCfg
				cfg.AccountPrivateKey = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
				return cfg
			},
			valid: true,
		},
		{
			name: "neither supplied",
			dstCfg: func() DestinationBlockchain {
				return dstCfg
			},
			valid: false,
		},
		{
			name: "both supplied",
			dstCfg: func() DestinationBlockchain {
				cfg := dstCfg
				cfg.KMSKeyID = kmsKey1
				cfg.KMSAWSRegion = awsRegion
				cfg.AccountPrivateKey = "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"
				return cfg
			},
			valid: false,
		},
		{
			name: "missing aws region",
			dstCfg: func() DestinationBlockchain {
				cfg := dstCfg
				cfg.KMSKeyID = kmsKey1
				// Missing AWS region
				return cfg
			},
			valid: false,
		},
	}
	for _, testCase := range testCases {
		dstCfg := testCase.dstCfg()
		err := dstCfg.Validate()
		if testCase.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestGetWarpQuorum(t *testing.T) {
	blockchainID, err := ids.FromString("p433wpuXyJiDhyazPYyZMJeaoPSW76CBZ2x7wrVPLgvokotXz")
	require.NoError(t, err)
	subnetID, err := ids.FromString("2PsShLjrFFwR51DMcAh8pyuwzLn1Ym3zRhuXLTmLCR1STk2mL6")
	require.NoError(t, err)

	testCases := []struct {
		name                string
		blockchainID        ids.ID
		subnetID            ids.ID
		chainConfig         params.ChainConfigWithUpgradesJSON
		getChainConfigCalls int
		expectedError       error
		expectedQuorum      WarpQuorum
	}{
		{
			name:                "primary network",
			blockchainID:        blockchainID,
			subnetID:            ids.Empty,
			getChainConfigCalls: 0,
			expectedError:       nil,
			expectedQuorum: WarpQuorum{
				QuorumNumerator:   warp.WarpDefaultQuorumNumerator,
				QuorumDenominator: warp.WarpQuorumDenominator,
			},
		},
		{
			name:                "subnet genesis precompile",
			blockchainID:        blockchainID,
			subnetID:            subnetID,
			getChainConfigCalls: 1,
			chainConfig: params.ChainConfigWithUpgradesJSON{
				ChainConfig: params.ChainConfig{
					GenesisPrecompiles: params.Precompiles{
						warpConfigKey: &warp.Config{
							QuorumNumerator: 0,
						},
					},
				},
			},
			expectedError: nil,
			expectedQuorum: WarpQuorum{
				QuorumNumerator:   warp.WarpDefaultQuorumNumerator,
				QuorumDenominator: warp.WarpQuorumDenominator,
			},
		},
		{
			name:                "subnet genesis precompile non-default",
			blockchainID:        blockchainID,
			subnetID:            subnetID,
			getChainConfigCalls: 1,
			chainConfig: params.ChainConfigWithUpgradesJSON{
				ChainConfig: params.ChainConfig{
					GenesisPrecompiles: params.Precompiles{
						warpConfigKey: &warp.Config{
							QuorumNumerator: 50,
						},
					},
				},
			},
			expectedError: nil,
			expectedQuorum: WarpQuorum{
				QuorumNumerator:   50,
				QuorumDenominator: warp.WarpQuorumDenominator,
			},
		},
		{
			name:                "subnet upgrade precompile",
			blockchainID:        blockchainID,
			subnetID:            subnetID,
			getChainConfigCalls: 1,
			chainConfig: params.ChainConfigWithUpgradesJSON{
				UpgradeConfig: params.UpgradeConfig{
					PrecompileUpgrades: []params.PrecompileUpgrade{
						{
							Config: &warp.Config{
								QuorumNumerator: 0,
							},
						},
					},
				},
			},
			expectedError: nil,
			expectedQuorum: WarpQuorum{
				QuorumNumerator:   warp.WarpDefaultQuorumNumerator,
				QuorumDenominator: warp.WarpQuorumDenominator,
			},
		},
		{
			name:                "subnet upgrade precompile non-default",
			blockchainID:        blockchainID,
			subnetID:            subnetID,
			getChainConfigCalls: 1,
			chainConfig: params.ChainConfigWithUpgradesJSON{
				UpgradeConfig: params.UpgradeConfig{
					PrecompileUpgrades: []params.PrecompileUpgrade{
						{
							Config: &warp.Config{
								QuorumNumerator: 50,
							},
						},
					},
				},
			},
			expectedError: nil,
			expectedQuorum: WarpQuorum{
				QuorumNumerator:   50,
				QuorumDenominator: warp.WarpQuorumDenominator,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := mock_ethclient.NewMockClient(gomock.NewController(t))
			gomock.InOrder(
				client.EXPECT().ChainConfig(gomock.Any()).Return(&testCase.chainConfig, nil).Times(testCase.getChainConfigCalls),
			)

			quorum, err := getWarpQuorum(testCase.subnetID, testCase.blockchainID, client)
			require.Equal(t, testCase.expectedError, err)
			require.Equal(t, testCase.expectedQuorum, quorum)
		})
	}
}

func TestValidateSourceBlockchain(t *testing.T) {
	validSourceCfg := SourceBlockchain{
		BlockchainID: testBlockchainID,
		RPCEndpoint: APIConfig{
			BaseURL: fmt.Sprintf("http://test.avax.network/ext/bc/%s/rpc", testBlockchainID),
		},
		WSEndpoint: APIConfig{
			BaseURL: fmt.Sprintf("ws://test.avax.network/ext/bc/%s/ws", testBlockchainID),
		},
		SubnetID: testSubnetID,
		VM:       "evm",
		SupportedDestinations: []*SupportedDestination{
			{
				BlockchainID: testBlockchainID,
			},
		},
		MessageContracts: map[string]MessageProtocolConfig{
			testAddress: {
				MessageFormat: TELEPORTER.String(),
			},
		},
	}
	testCases := []struct {
		name                          string
		sourceSubnet                  func() SourceBlockchain
		destinationBlockchainIDs      []string
		expectError                   bool
		expectedSupportedDestinations []string
	}{
		{
			name:                          "valid source subnet; explicitly supported destination",
			sourceSubnet:                  func() SourceBlockchain { return validSourceCfg },
			destinationBlockchainIDs:      []string{testBlockchainID},
			expectError:                   false,
			expectedSupportedDestinations: []string{testBlockchainID},
		},
		{
			name: "valid source subnet; implicitly supported destination",
			sourceSubnet: func() SourceBlockchain {
				cfg := validSourceCfg
				cfg.SupportedDestinations = nil
				return cfg
			},
			destinationBlockchainIDs:      []string{testBlockchainID},
			expectError:                   false,
			expectedSupportedDestinations: []string{testBlockchainID},
		},
		{
			name:                          "valid source subnet; partially supported destinations",
			sourceSubnet:                  func() SourceBlockchain { return validSourceCfg },
			destinationBlockchainIDs:      []string{testBlockchainID, testBlockchainID2},
			expectError:                   false,
			expectedSupportedDestinations: []string{testBlockchainID},
		},
		{
			name:                          "valid source subnet; unsupported destinations",
			sourceSubnet:                  func() SourceBlockchain { return validSourceCfg },
			destinationBlockchainIDs:      []string{testBlockchainID2},
			expectError:                   true,
			expectedSupportedDestinations: []string{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			blockchainIDs := set.NewSet[string](len(testCase.destinationBlockchainIDs))
			for _, id := range testCase.destinationBlockchainIDs {
				blockchainIDs.Add(id)
			}

			sourceSubnet := testCase.sourceSubnet()
			res := sourceSubnet.Validate(&blockchainIDs)
			if testCase.expectError {
				require.Error(t, res)
			} else {
				require.NoError(t, res)
			}
			// check the supported destinations
			for _, idStr := range testCase.expectedSupportedDestinations {
				id, err := ids.FromString(idStr)
				require.NoError(t, err)
				require.True(t, func() bool {
					for _, dest := range sourceSubnet.SupportedDestinations {
						if dest.GetBlockchainID() == id {
							return true
						}
					}
					return false
				}())
			}
		})
	}
}
