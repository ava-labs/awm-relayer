// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/viper"
)

// global config singleton
var globalConfig Config

const (
	relayerPrivateKeyBytes      = 32
	accountPrivateKeyEnvVarName = "ACCOUNT_PRIVATE_KEY"
	cChainIdentifierString      = "C"
)

var (
	ErrInvalidPrivateKey = errors.New("failed to set private key string")
)

type MessageProtocolConfig struct {
	MessageFormat string                 `mapstructure:"message-format" json:"message-format"`
	Settings      map[string]interface{} `mapstructure:"settings" json:"settings"`
}
type SourceSubnet struct {
	SubnetID              string                           `mapstructure:"subnet-id" json:"subnet-id"`
	ChainID               string                           `mapstructure:"chain-id" json:"chain-id"`
	VM                    string                           `mapstructure:"vm" json:"vm"`
	APINodeHost           string                           `mapstructure:"api-node-host" json:"api-node-host"`
	APINodePort           uint32                           `mapstructure:"api-node-port" json:"api-node-port"`
	EncryptConnection     bool                             `mapstructure:"encrypt-connection" json:"encrypt-connection"`
	RPCEndpoint           string                           `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`
	WSEndpoint            string                           `mapstructure:"ws-endpoint" json:"ws-endpoint"`
	MessageContracts      map[string]MessageProtocolConfig `mapstructure:"message-contracts" json:"message-contracts"`
	SupportedDestinations []string                         `mapstructure:"supported-destinations" json:"allowed-destinations"`

	// convenience field
	supportedDestinationsMap map[ids.ID]bool
}

type DestinationSubnet struct {
	SubnetID          string `mapstructure:"subnet-id" json:"subnet-id"`
	ChainID           string `mapstructure:"chain-id" json:"chain-id"`
	VM                string `mapstructure:"vm" json:"vm"`
	APINodeHost       string `mapstructure:"api-node-host" json:"api-node-host"`
	APINodePort       uint32 `mapstructure:"api-node-port" json:"api-node-port"`
	EncryptConnection bool   `mapstructure:"encrypt-connection" json:"encrypt-connection"`
	RPCEndpoint       string `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`
	AccountPrivateKey string `mapstructure:"account-private-key" json:"account-private-key"`
}

type Config struct {
	LogLevel           string              `mapstructure:"log-level" json:"log-level"`
	NetworkID          uint32              `mapstructure:"network-id" json:"network-id"`
	PChainAPIURL       string              `mapstructure:"p-chain-api-url" json:"p-chain-api-url"`
	EncryptConnection  bool                `mapstructure:"encrypt-connection" json:"encrypt-connection"`
	StorageLocation    string              `mapstructure:"storage-location" json:"storage-location"`
	SourceSubnets      []SourceSubnet      `mapstructure:"source-subnets" json:"source-subnets"`
	DestinationSubnets []DestinationSubnet `mapstructure:"destination-subnets" json:"destination-subnets"`
}

func SetDefaultConfigValues(v *viper.Viper) {
	v.SetDefault(LogLevelKey, logging.Info.String())
	v.SetDefault(NetworkIDKey, constants.MainnetID)
	v.SetDefault(PChainAPIURLKey, "https://api.avax.network")
	v.SetDefault(EncryptConnectionKey, true)
	v.SetDefault(StorageLocationKey, "./.awm-relayer-storage")
}

// BuildConfig constructs the relayer config using Viper.
// The following precedence order is used. Each item takes precedence over the item below it:
//  1. Flags
//  2. Environment variables
//     a. Global account-private-key
//     b. Chain-specific account-private-key
//  3. Config file
//
// Returns the Config option and a bool indicating whether any options provided from one source
// were explicitly overridden by a higher precedence source.
// TODO: Improve the optionOverwritten return value to reflect the key that was modified.
func BuildConfig(v *viper.Viper) (Config, bool, error) {
	// Set default values
	SetDefaultConfigValues(v)

	// Build the config from Viper
	var (
		cfg               Config
		err               error
		optionOverwritten bool = false
	)

	cfg.LogLevel = v.GetString(LogLevelKey)
	cfg.NetworkID = v.GetUint32(NetworkIDKey)
	cfg.PChainAPIURL = v.GetString(PChainAPIURLKey)
	cfg.EncryptConnection = v.GetBool(EncryptConnectionKey)
	cfg.StorageLocation = v.GetString(StorageLocationKey)
	if err := v.UnmarshalKey(DestinationSubnetsKey, &cfg.DestinationSubnets); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal destination subnets: %v", err)
	}
	if err := v.UnmarshalKey(SourceSubnetsKey, &cfg.SourceSubnets); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal source subnets: %v", err)
	}

	// Explicitly overwrite the configured account private key
	// If account-private-key is set as a flag or environment variable,
	// overwrite all destination subnet configurations to use that key
	// In all cases, sanitize the key before setting it in the config
	accountPrivateKey := v.GetString(AccountPrivateKeyKey)
	if accountPrivateKey != "" {
		optionOverwritten = true
		for i := range cfg.DestinationSubnets {
			cfg.DestinationSubnets[i].AccountPrivateKey = utils.SanitizeHexString(accountPrivateKey)
		}
	} else {
		// Otherwise, check for private keys suffixed with the chain ID and set it for that subnet
		// Since the key is dynamic, this is only possible through environment variables
		for i, subnet := range cfg.DestinationSubnets {
			subnetAccountPrivateKey := os.Getenv(fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, subnet.ChainID))
			if subnetAccountPrivateKey != "" {
				optionOverwritten = true
				cfg.DestinationSubnets[i].AccountPrivateKey = utils.SanitizeHexString(subnetAccountPrivateKey)
			} else {
				cfg.DestinationSubnets[i].AccountPrivateKey = utils.SanitizeHexString(cfg.DestinationSubnets[i].AccountPrivateKey)
			}
		}
	}

	if err = cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("failed to validate configuration: %v", err)
	}

	var protocol string
	if cfg.EncryptConnection {
		protocol = "https"
	} else {
		protocol = "http"
	}

	pChainapiUrl, err := utils.ConvertProtocol(cfg.PChainAPIURL, protocol)
	if err != nil {
		return Config{}, false, err
	}
	cfg.PChainAPIURL = pChainapiUrl

	globalConfig = cfg

	return cfg, optionOverwritten, nil
}

func (c *Config) Validate() error {
	if len(c.SourceSubnets) == 0 {
		return fmt.Errorf("relayer not configured to relay from any subnets. A list of source subnets must be provided in the configuration file")
	}
	if len(c.DestinationSubnets) == 0 {
		return fmt.Errorf("relayer not configured to relay to any subnets. A list of destination subnets must be provided in the configuration file")
	}
	if _, err := url.ParseRequestURI(c.PChainAPIURL); err != nil {
		return err
	}

	// Validate the destination chains
	destinationChains := set.NewSet[string](len(c.DestinationSubnets))
	for _, s := range c.DestinationSubnets {
		if err := s.Validate(); err != nil {
			return err
		}
		if destinationChains.Contains(s.ChainID) {
			return fmt.Errorf("configured destination subnets must have unique chain IDs")
		}
		destinationChains.Add(s.ChainID)
	}

	// Validate the source chains, and validate that the allowed destinations are configured as destinations
	sourceChains := set.NewSet[string](len(c.SourceSubnets))
	for _, s := range c.SourceSubnets {
		if err := s.Validate(); err != nil {
			return err
		}
		if sourceChains.Contains(s.ChainID) {
			return fmt.Errorf("configured source subnets must have unique chain IDs")
		}
		sourceChains.Add(s.ChainID)

		for _, blockchainID := range s.SupportedDestinations {
			if !destinationChains.Contains(blockchainID) {
				return fmt.Errorf("configured source subnet %s has a supported destination blockchain ID %s that is not configured as a destination blockchain",
					s.SubnetID,
					blockchainID)
			}
		}
	}

	return nil
}

func (s *SourceSubnet) GetSupportedDestinations() map[ids.ID]bool {
	return s.supportedDestinationsMap
}

func (s *SourceSubnet) Validate() error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in source subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.ChainID); err != nil {
		return fmt.Errorf("invalid chainID in source subnet configuration. Provided ID: %s", s.ChainID)
	}
	if _, err := url.ParseRequestURI(s.GetNodeWSEndpoint()); err != nil {
		return fmt.Errorf("invalid relayer subscribe URL in source subnet configuration: %v", err)
	}
	if _, err := url.ParseRequestURI(s.GetNodeRPCEndpoint()); err != nil {
		return fmt.Errorf("invalid relayer RPC URL in source subnet configuration: %v", err)
	}

	// Validate the VM specific settings
	switch ParseVM(s.VM) {
	case EVM:
		for messageContractAddress := range s.MessageContracts {
			if !common.IsHexAddress(messageContractAddress) {
				return fmt.Errorf("invalid message contract address in EVM source subnet: %s", messageContractAddress)
			}
		}
	default:
		return fmt.Errorf("unsupported VM type for source subnet: %v", s.VM)
	}

	// Validate message settings correspond to a supported message protocol
	for _, messageConfig := range s.MessageContracts {
		protocol := ParseMessageProtocol(messageConfig.MessageFormat)
		if protocol == UNKNOWN_MESSAGE_PROTOCOL {
			return fmt.Errorf("unsupported message protocol for source subnet: %s", messageConfig.MessageFormat)
		}
	}

	// Validate the allowed destinations
	for _, chainIDs := range s.SupportedDestinations {
		if _, err := ids.FromString(chainIDs); err != nil {
			return fmt.Errorf("invalid chainID in source subnet configuration. Provided ID: %s", chainIDs)
		}
	}

	// Store the allowed destinations for future use
	s.supportedDestinationsMap = make(map[ids.ID]bool)
	for _, chainIDStr := range s.SupportedDestinations {
		chainID, err := ids.FromString(chainIDStr)
		if err != nil {
			return fmt.Errorf("invalid chainID in configuration. error: %v", err)
		}
		s.supportedDestinationsMap[chainID] = true
	}

	return nil
}

func (s *DestinationSubnet) Validate() error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in source subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.ChainID); err != nil {
		return fmt.Errorf("invalid chainID in source subnet configuration. Provided ID: %s", s.ChainID)
	}
	if _, err := url.ParseRequestURI(s.GetNodeRPCEndpoint()); err != nil {
		return fmt.Errorf("invalid relayer broadcast URL: %v", err)
	}

	if len(s.AccountPrivateKey) != relayerPrivateKeyBytes*2 {
		return fmt.Errorf("invalid account private key hex string")
	}

	if _, err := hex.DecodeString(s.AccountPrivateKey); err != nil {
		return fmt.Errorf("invalid account private key hex string: %v", err)
	}

	// Validate the VM specific settings
	vm := ParseVM(s.VM)
	if vm == UNKNOWN_VM {
		return fmt.Errorf("unsupported VM type for source subnet: %s", s.VM)
	}

	return nil
}

func constructURL(protocol string, host string, port uint32, encrypt bool, chainIDStr string, subnetIDStr string) string {
	var protocolPathMap = map[string]string{
		"http": "rpc",
		"ws":   "ws",
	}
	path := protocolPathMap[protocol]

	if encrypt {
		protocol = protocol + "s"
	}
	portStr := ""
	if port != 0 {
		portStr = fmt.Sprintf(":%d", port)
	}
	subnetID, _ := ids.FromString(subnetIDStr) // already validated in Validate()
	if subnetID == constants.PrimaryNetworkID {
		chainIDStr = cChainIdentifierString
	}
	return fmt.Sprintf("%s://%s%s/ext/bc/%s/%s", protocol, host, portStr, chainIDStr, path)
}

// Constructs an RPC endpoint for the subnet.
// If the RPCEndpoint field is set in the configuration, returns that directly.
// Otherwise, constructs the endpoint from the APINodeHost, APINodePort, and EncryptConnection fields,
// following the /ext/bc/{chainID}/rpc format.
func (s *DestinationSubnet) GetNodeRPCEndpoint() string {
	if s.RPCEndpoint != "" {
		return s.RPCEndpoint
	}

	// Save this result for future use
	s.RPCEndpoint = constructURL(
		"http",
		s.APINodeHost,
		s.APINodePort,
		s.EncryptConnection,
		s.ChainID,
		s.SubnetID,
	)
	return s.RPCEndpoint
}

// Constructs an RPC endpoint for the subnet.
// If the RPCEndpoint field is set in the configuration, returns that directly.
// Otherwise, constructs the endpoint from the APINodeHost, APINodePort, and EncryptConnection fields,
// following the /ext/bc/{chainID}/rpc format.
func (s *SourceSubnet) GetNodeRPCEndpoint() string {
	if s.RPCEndpoint != "" {
		return s.RPCEndpoint
	}

	// Save this result for future use
	s.RPCEndpoint = constructURL(
		"http",
		s.APINodeHost,
		s.APINodePort,
		s.EncryptConnection,
		s.ChainID,
		s.SubnetID,
	)
	return s.RPCEndpoint
}

// Constructs a WS endpoint for the subnet.
// If the WSEndpoint field is set in the configuration, returns that directly.
// Otherwise, constructs the endpoint from the APINodeHost, APINodePort, and EncryptConnection fields,
// following the /ext/bc/{chainID}/ws format.
func (s *SourceSubnet) GetNodeWSEndpoint() string {
	if s.WSEndpoint != "" {
		return s.WSEndpoint
	}

	// Save this result for future use
	s.WSEndpoint = constructURL(
		"ws",
		s.APINodeHost,
		s.APINodePort,
		s.EncryptConnection,
		s.ChainID,
		s.SubnetID,
	)
	return s.WSEndpoint
}

// Get the private key and derive the wallet address from a relayer's configured private key for a given destination subnet.
func (s *DestinationSubnet) GetRelayerAccountInfo() (*ecdsa.PrivateKey, common.Address, error) {
	var ok bool
	pk := new(ecdsa.PrivateKey)
	pk.D, ok = new(big.Int).SetString(s.AccountPrivateKey, 16)
	if !ok {
		return nil, common.Address{}, ErrInvalidPrivateKey
	}
	pk.PublicKey.Curve = crypto.S256()
	pk.PublicKey.X, pk.PublicKey.Y = pk.PublicKey.Curve.ScalarBaseMult(pk.D.Bytes())
	pkBytes := pk.PublicKey.X.Bytes()
	pkBytes = append(pkBytes, pk.PublicKey.Y.Bytes()...)
	return pk, common.BytesToAddress(crypto.Keccak256(pkBytes)), nil
}

//
// Global config getters
//

// GetSourceIDs returns the Subnet and Chain IDs of all subnets configured as a source
func GetSourceIDs() ([]ids.ID, []ids.ID, error) {
	var sourceSubnetIDs []ids.ID
	var sourceChainIDs []ids.ID
	for _, s := range globalConfig.SourceSubnets {
		subnetID, err := ids.FromString(s.SubnetID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid subnetID in configuration. error: %v", err)
		}
		sourceSubnetIDs = append(sourceSubnetIDs, subnetID)

		chainID, err := ids.FromString(s.ChainID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid subnetID in configuration. error: %v", err)
		}
		sourceChainIDs = append(sourceChainIDs, chainID)
	}
	return sourceSubnetIDs, sourceChainIDs, nil
}
