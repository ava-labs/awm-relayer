// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/utils"

	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/precompile/contracts/warp"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/subnet-evm/precompile/registry"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/viper"
)

const (
	relayerPrivateKeyBytes      = 32
	accountPrivateKeyEnvVarName = "ACCOUNT_PRIVATE_KEY"
	cChainIdentifierString      = "C"
	warpConfigKey               = "warpConfig"
)

var errFailedToGetWarpQuorum = errors.New("failed to get warp quorum")

// The generic configuration for a message protocol.
type MessageProtocolConfig struct {
	MessageFormat string                 `mapstructure:"message-format" json:"message-format"`
	Settings      map[string]interface{} `mapstructure:"settings" json:"settings"`
}

// Defines a manual warp message to be sent from the relayer on startup.
type ManualWarpMessage struct {
	UnsignedMessageBytes    string `mapstructure:"unsigned-message-bytes" json:"unsigned-message-bytes"`
	SourceBlockchainID      string `mapstructure:"source-blockchain-id" json:"source-blockchain-id"`
	DestinationBlockchainID string `mapstructure:"destination-blockchain-id" json:"destination-blockchain-id"`
	SourceAddress           string `mapstructure:"source-address" json:"source-address"`
	DestinationAddress      string `mapstructure:"destination-address" json:"destination-address"`

	// convenience fields to access the values after initialization
	unsignedMessageBytes    []byte
	sourceBlockchainID      ids.ID
	destinationBlockchainID ids.ID
	sourceAddress           common.Address
	destinationAddress      common.Address
}

// Specifies a supported destination blockchain and addresses for a source blockchain.
type SupportedDestination struct {
	BlockchainID string   `mapstructure:"blockchain-id" json:"blockchain-id"`
	Addresses    []string `mapstructure:"addresses" json:"addresses"`

	// convenience fields to access parsed data after initialization
	blockchainID ids.ID
	addresses    []common.Address
}

func (s *SupportedDestination) GetBlockchainID() ids.ID {
	return s.blockchainID
}

func (s *SupportedDestination) GetAddresses() []common.Address {
	return s.addresses
}

// Source blockchain configuration.
// Specifies how to connect to and listen for messages on the source blockchain.
// Specifies the message protocols supported by the relayer for this blockchain.
// Specifies the supported source addresses, and destination blockchains and addresses.
// Specifies the height from which to start processing historical blocks.
type SourceBlockchain struct {
	SubnetID                          string                           `mapstructure:"subnet-id" json:"subnet-id"`
	BlockchainID                      string                           `mapstructure:"blockchain-id" json:"blockchain-id"`
	VM                                string                           `mapstructure:"vm" json:"vm"`
	RPCEndpoint                       string                           `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`
	WSEndpoint                        string                           `mapstructure:"ws-endpoint" json:"ws-endpoint"`
	MessageContracts                  map[string]MessageProtocolConfig `mapstructure:"message-contracts" json:"message-contracts"`
	SupportedDestinations             []*SupportedDestination          `mapstructure:"supported-destinations" json:"supported-destinations"`
	ProcessHistoricalBlocksFromHeight uint64                           `mapstructure:"process-historical-blocks-from-height" json:"process-historical-blocks-from-height"`
	AllowedOriginSenderAddresses      []string                         `mapstructure:"allowed-origin-sender-addresses" json:"allowed-origin-sender-addresses"`

	// convenience fields to access parsed data after initialization
	subnetID                     ids.ID
	blockchainID                 ids.ID
	allowedOriginSenderAddresses []common.Address
}

// Destination blockchain configuration. Specifies how to connect to and issue transactions on the desination blockchain.
type DestinationBlockchain struct {
	SubnetID          string `mapstructure:"subnet-id" json:"subnet-id"`
	BlockchainID      string `mapstructure:"blockchain-id" json:"blockchain-id"`
	VM                string `mapstructure:"vm" json:"vm"`
	RPCEndpoint       string `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`
	AccountPrivateKey string `mapstructure:"account-private-key" json:"account-private-key"`

	// Fetched from the chain after startup
	warpQuorum WarpQuorum

	// convenience fields to access parsed data after initialization
	subnetID     ids.ID
	blockchainID ids.ID
}

// Warp Quorum configuration, fetched from the chain config
type WarpQuorum struct {
	QuorumNumerator   uint64
	QuorumDenominator uint64
}

// Top-level configuration
type Config struct {
	LogLevel        string `mapstructure:"log-level" json:"log-level"`
	PChainAPIURL    string `mapstructure:"p-chain-api-url" json:"p-chain-api-url"`
	InfoAPIURL      string `mapstructure:"info-api-url" json:"info-api-url"`
	StorageLocation string `mapstructure:"storage-location" json:"storage-location"`
	APIPort         uint16 `mapstructure:"api-port" json:"api-port"`
	MetricsPort     uint16 `mapstructure:"metrics-port" json:"metrics-port"`

	SourceBlockchains      []*SourceBlockchain      `mapstructure:"source-blockchains" json:"source-blockchains"`
	DestinationBlockchains []*DestinationBlockchain `mapstructure:"destination-blockchains" json:"destination-blockchains"`
	ProcessMissedBlocks    bool                     `mapstructure:"process-missed-blocks" json:"process-missed-blocks"`
	ManualWarpMessages     []*ManualWarpMessage     `mapstructure:"manual-warp-messages" json:"manual-warp-messages"`

	// convenience field to fetch a blockchain's subnet ID
	blockchainIDToSubnetID map[ids.ID]ids.ID
}

func SetDefaultConfigValues(v *viper.Viper) {
	v.SetDefault(LogLevelKey, logging.Info.String())
	v.SetDefault(StorageLocationKey, "./.awm-relayer-storage")
	v.SetDefault(ProcessMissedBlocksKey, true)
	v.SetDefault(APIPortKey, 8080)
	v.SetDefault(MetricsPortKey, 9090)
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
	cfg.PChainAPIURL = v.GetString(PChainAPIURLKey)
	cfg.InfoAPIURL = v.GetString(InfoAPIURLKey)
	cfg.StorageLocation = v.GetString(StorageLocationKey)
	cfg.ProcessMissedBlocks = v.GetBool(ProcessMissedBlocksKey)
	cfg.APIPort = v.GetUint16(APIPortKey)
	cfg.MetricsPort = v.GetUint16(MetricsPortKey)
	if err := v.UnmarshalKey(ManualWarpMessagesKey, &cfg.ManualWarpMessages); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal manual warp messages: %w", err)
	}
	if err := v.UnmarshalKey(DestinationBlockchainsKey, &cfg.DestinationBlockchains); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal destination subnets: %w", err)
	}
	if err := v.UnmarshalKey(SourceBlockchainsKey, &cfg.SourceBlockchains); err != nil {
		return Config{}, false, fmt.Errorf("failed to unmarshal source subnets: %w", err)
	}

	// Explicitly overwrite the configured account private key
	// If account-private-key is set as a flag or environment variable,
	// overwrite all destination subnet configurations to use that key
	// In all cases, sanitize the key before setting it in the config
	accountPrivateKey := v.GetString(AccountPrivateKeyKey)
	if accountPrivateKey != "" {
		optionOverwritten = true
		for i := range cfg.DestinationBlockchains {
			cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(accountPrivateKey)
		}
	} else {
		// Otherwise, check for private keys suffixed with the chain ID and set it for that subnet
		// Since the key is dynamic, this is only possible through environment variables
		for i, subnet := range cfg.DestinationBlockchains {
			subnetAccountPrivateKey := os.Getenv(fmt.Sprintf("%s_%s", accountPrivateKeyEnvVarName, subnet.BlockchainID))
			if subnetAccountPrivateKey != "" {
				optionOverwritten = true
				cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(subnetAccountPrivateKey)
			} else {
				cfg.DestinationBlockchains[i].AccountPrivateKey = utils.SanitizeHexString(cfg.DestinationBlockchains[i].AccountPrivateKey)
			}
		}
	}

	if err = cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("failed to validate configuration: %w", err)
	}

	return cfg, optionOverwritten, nil
}

// Validates the configuration
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters
func (c *Config) Validate() error {
	if len(c.SourceBlockchains) == 0 {
		return errors.New("relayer not configured to relay from any subnets. A list of source subnets must be provided in the configuration file")
	}
	if len(c.DestinationBlockchains) == 0 {
		return errors.New("relayer not configured to relay to any subnets. A list of destination subnets must be provided in the configuration file")
	}
	if _, err := url.ParseRequestURI(c.PChainAPIURL); err != nil {
		return err
	}
	if _, err := url.ParseRequestURI(c.InfoAPIURL); err != nil {
		return err
	}

	blockchainIDToSubnetID := make(map[ids.ID]ids.ID)

	// Validate the destination chains
	destinationChains := set.NewSet[string](len(c.DestinationBlockchains))
	for _, s := range c.DestinationBlockchains {
		if err := s.Validate(); err != nil {
			return err
		}
		if destinationChains.Contains(s.BlockchainID) {
			return errors.New("configured destination subnets must have unique chain IDs")
		}
		destinationChains.Add(s.BlockchainID)
		blockchainIDToSubnetID[s.blockchainID] = s.subnetID
	}

	// Validate the source chains and store the source subnet and chain IDs for future use
	sourceBlockchains := set.NewSet[string](len(c.SourceBlockchains))
	for _, s := range c.SourceBlockchains {
		// Validate configuration
		if err := s.Validate(&destinationChains); err != nil {
			return err
		}
		// Verify uniqueness
		if sourceBlockchains.Contains(s.BlockchainID) {
			return errors.New("configured source subnets must have unique chain IDs")
		}
		sourceBlockchains.Add(s.BlockchainID)
		blockchainIDToSubnetID[s.blockchainID] = s.subnetID
	}
	c.blockchainIDToSubnetID = blockchainIDToSubnetID

	// Validate the manual warp messages
	for i, msg := range c.ManualWarpMessages {
		if err := msg.Validate(); err != nil {
			return fmt.Errorf("invalid manual warp message at index %d: %w", i, err)
		}
	}

	return nil
}

func (c *Config) GetSubnetID(blockchainID ids.ID) ids.ID {
	return c.blockchainIDToSubnetID[blockchainID]
}

func (m *ManualWarpMessage) GetUnsignedMessageBytes() []byte {
	return m.unsignedMessageBytes
}

func (m *ManualWarpMessage) GetSourceBlockchainID() ids.ID {
	return m.sourceBlockchainID
}

func (m *ManualWarpMessage) GetSourceAddress() common.Address {
	return m.sourceAddress
}

func (m *ManualWarpMessage) GetDestinationBlockchainID() ids.ID {
	return m.destinationBlockchainID
}

func (m *ManualWarpMessage) GetDestinationAddress() common.Address {
	return m.destinationAddress
}

// Validates the manual Warp message configuration.
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters
func (m *ManualWarpMessage) Validate() error {
	unsignedMsg, err := hex.DecodeString(utils.SanitizeHexString(m.UnsignedMessageBytes))
	if err != nil {
		return err
	}
	sourceBlockchainID, err := ids.FromString(m.SourceBlockchainID)
	if err != nil {
		return err
	}
	if !common.IsHexAddress(m.SourceAddress) {
		return errors.New("invalid source address in manual warp message configuration")
	}
	destinationBlockchainID, err := ids.FromString(m.DestinationBlockchainID)
	if err != nil {
		return err
	}
	if !common.IsHexAddress(m.DestinationAddress) {
		return errors.New("invalid destination address in manual warp message configuration")
	}
	m.unsignedMessageBytes = unsignedMsg
	m.sourceBlockchainID = sourceBlockchainID
	m.sourceAddress = common.HexToAddress(m.SourceAddress)
	m.destinationBlockchainID = destinationBlockchainID
	m.destinationAddress = common.HexToAddress(m.DestinationAddress)
	return nil
}

// If the numerator in the Warp config is 0, use the default value
func calculateQuorumNumerator(cfgNumerator uint64) uint64 {
	if cfgNumerator == 0 {
		return warp.WarpDefaultQuorumNumerator
	}
	return cfgNumerator
}

// Helper to retrieve the Warp Quorum from the chain config.
// Differentiates between subnet-evm and coreth RPC internally
func getWarpQuorum(
	subnetID ids.ID,
	blockchainID ids.ID,
	client ethclient.Client,
) (WarpQuorum, error) {
	if subnetID == constants.PrimaryNetworkID {
		return WarpQuorum{
			QuorumNumerator:   warp.WarpDefaultQuorumNumerator,
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}

	// Fetch the subnet's chain config
	chainConfig, err := client.ChainConfig(context.Background())
	if err != nil {
		return WarpQuorum{}, fmt.Errorf("failed to fetch chain config for blockchain %s: %w", blockchainID, err)
	}

	// First, check the list of precompile upgrades to get the most up to date Warp config
	// We only need to consider the most recent Warp config, since the QuorumNumerator is used
	// at signature verification time on the receiving chain, regardless of the Warp config at the
	// time of the message's creation
	var warpConfig *warp.Config
	for _, precompile := range chainConfig.UpgradeConfig.PrecompileUpgrades {
		cfg, ok := precompile.Config.(*warp.Config)
		if !ok {
			continue
		}
		if warpConfig == nil {
			warpConfig = cfg
			continue
		}
		if *cfg.Timestamp() > *warpConfig.Timestamp() {
			warpConfig = cfg
		}
	}
	if warpConfig != nil {
		return WarpQuorum{
			QuorumNumerator:   calculateQuorumNumerator(warpConfig.QuorumNumerator),
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}

	// If we didn't find the Warp config in the upgrade precompile list, check the genesis config
	warpConfig, ok := chainConfig.GenesisPrecompiles[warpConfigKey].(*warp.Config)
	if ok {
		return WarpQuorum{
			QuorumNumerator:   calculateQuorumNumerator(warpConfig.QuorumNumerator),
			QuorumDenominator: warp.WarpQuorumDenominator,
		}, nil
	}
	return WarpQuorum{}, fmt.Errorf("failed to find warp config for blockchain %s", blockchainID)
}

func (c *Config) InitializeWarpQuorums() error {
	// Fetch the Warp quorum values for each destination subnet.
	for _, destinationSubnet := range c.DestinationBlockchains {
		err := destinationSubnet.initializeWarpQuorum()
		if err != nil {
			return fmt.Errorf("failed to initialize Warp quorum for destination subnet %s: %w", destinationSubnet.SubnetID, err)
		}
	}

	return nil
}

// Validates the source subnet configuration, including verifying that the supported destinations are present in destinationBlockchainIDs
// Does not modify the public fields as derived from the configuration passed to the application,
// but does initialize private fields available through getters
func (s *SourceBlockchain) Validate(destinationBlockchainIDs *set.Set[string]) error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in source subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.BlockchainID); err != nil {
		return fmt.Errorf("invalid blockchainID in source subnet configuration. Provided ID: %s", s.BlockchainID)
	}
	if _, err := url.ParseRequestURI(s.WSEndpoint); err != nil {
		return fmt.Errorf("invalid relayer subscribe URL in source subnet configuration: %w", err)
	}
	if _, err := url.ParseRequestURI(s.RPCEndpoint); err != nil {
		return fmt.Errorf("invalid relayer RPC URL in source subnet configuration: %w", err)
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
		return fmt.Errorf("unsupported VM type for source subnet: %s", s.VM)
	}

	// Validate message settings correspond to a supported message protocol
	for _, messageConfig := range s.MessageContracts {
		protocol := ParseMessageProtocol(messageConfig.MessageFormat)
		if protocol == UNKNOWN_MESSAGE_PROTOCOL {
			return fmt.Errorf("unsupported message protocol for source subnet: %s", messageConfig.MessageFormat)
		}
	}

	// Validate and store the subnet and blockchain IDs for future use
	blockchainID, err := ids.FromString(s.BlockchainID)
	if err != nil {
		return fmt.Errorf("invalid blockchainID in configuration. error: %w", err)
	}
	s.blockchainID = blockchainID
	subnetID, err := ids.FromString(s.SubnetID)
	if err != nil {
		return fmt.Errorf("invalid subnetID in configuration. error: %w", err)
	}
	s.subnetID = subnetID

	// If the list of supported destinations is empty, populate with all of the configured destinations
	if len(s.SupportedDestinations) == 0 {
		for _, blockchainIDStr := range destinationBlockchainIDs.List() {
			s.SupportedDestinations = append(s.SupportedDestinations, &SupportedDestination{
				BlockchainID: blockchainIDStr,
			})
		}
	}
	for _, dest := range s.SupportedDestinations {
		blockchainID, err := ids.FromString(dest.BlockchainID)
		if err != nil {
			return fmt.Errorf("invalid blockchainID in configuration. error: %w", err)
		}
		if !destinationBlockchainIDs.Contains(dest.BlockchainID) {
			return fmt.Errorf("configured source subnet %s has a supported destination blockchain ID %s that is not configured as a destination blockchain",
				s.SubnetID,
				blockchainID)
		}
		dest.blockchainID = blockchainID
		for _, addressStr := range dest.Addresses {
			if !common.IsHexAddress(addressStr) {
				return fmt.Errorf("invalid allowed destination address in source blockchain configuration: %s", addressStr)
			}
			address := common.HexToAddress(addressStr)
			if address == utils.ZeroAddress {
				return fmt.Errorf("invalid allowed destination address in source blockchain configuration: %s", addressStr)
			}
			dest.addresses = append(dest.addresses, address)
		}
	}

	// Validate and store the allowed origin source addresses
	allowedOriginSenderAddresses := make([]common.Address, len(s.AllowedOriginSenderAddresses))
	for i, addressStr := range s.AllowedOriginSenderAddresses {
		if !common.IsHexAddress(addressStr) {
			return fmt.Errorf("invalid allowed origin sender address in source blockchain configuration: %s", addressStr)
		}
		address := common.HexToAddress(addressStr)
		if address == utils.ZeroAddress {
			return fmt.Errorf("invalid allowed origin sender address in source blockchain configuration: %s", addressStr)
		}
		allowedOriginSenderAddresses[i] = address
	}
	s.allowedOriginSenderAddresses = allowedOriginSenderAddresses

	return nil
}

func (s *SourceBlockchain) GetSubnetID() ids.ID {
	return s.subnetID
}

func (s *SourceBlockchain) GetBlockchainID() ids.ID {
	return s.blockchainID
}

func (s *SourceBlockchain) GetAllowedOriginSenderAddresses() []common.Address {
	return s.allowedOriginSenderAddresses
}

// Validatees the destination subnet configuration
func (s *DestinationBlockchain) Validate() error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in source subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.BlockchainID); err != nil {
		return fmt.Errorf("invalid blockchainID in source subnet configuration. Provided ID: %s", s.BlockchainID)
	}
	if _, err := url.ParseRequestURI(s.RPCEndpoint); err != nil {
		return fmt.Errorf("invalid relayer broadcast URL: %w", err)
	}

	if len(s.AccountPrivateKey) != relayerPrivateKeyBytes*2 {
		return errors.New("invalid account private key hex string")
	}

	if _, err := hex.DecodeString(s.AccountPrivateKey); err != nil {
		return fmt.Errorf("invalid account private key hex string: %w", err)
	}

	// Validate the VM specific settings
	vm := ParseVM(s.VM)
	if vm == UNKNOWN_VM {
		return fmt.Errorf("unsupported VM type for source subnet: %s", s.VM)
	}

	// Validate and store the subnet and blockchain IDs for future use
	blockchainID, err := ids.FromString(s.BlockchainID)
	if err != nil {
		return fmt.Errorf("invalid blockchainID in configuration. error: %w", err)
	}
	s.blockchainID = blockchainID
	subnetID, err := ids.FromString(s.SubnetID)
	if err != nil {
		return fmt.Errorf("invalid subnetID in configuration. error: %w", err)
	}
	s.subnetID = subnetID

	return nil
}

func (s *DestinationBlockchain) GetSubnetID() ids.ID {
	return s.subnetID
}

func (s *DestinationBlockchain) GetBlockchainID() ids.ID {
	return s.blockchainID
}

func (s *DestinationBlockchain) initializeWarpQuorum() error {
	blockchainID, err := ids.FromString(s.BlockchainID)
	if err != nil {
		return fmt.Errorf("invalid blockchainID in configuration. error: %w", err)
	}
	subnetID, err := ids.FromString(s.SubnetID)
	if err != nil {
		return fmt.Errorf("invalid subnetID in configuration. error: %w", err)
	}

	client, err := ethclient.Dial(s.RPCEndpoint)
	if err != nil {
		return fmt.Errorf("failed to dial destination blockchain %s: %w", blockchainID, err)
	}
	defer client.Close()
	quorum, err := getWarpQuorum(subnetID, blockchainID, client)
	if err != nil {
		return fmt.Errorf("failed to fetch warp quorum for subnet %s: %w", subnetID, err)
	}

	s.warpQuorum = quorum
	return nil
}

// Get the private key and derive the wallet address from a relayer's configured private key for a given destination subnet.
func (s *DestinationBlockchain) GetRelayerAccountInfo() (*ecdsa.PrivateKey, common.Address, error) {
	pk, err := crypto.HexToECDSA(s.AccountPrivateKey)
	if err != nil {
		return nil, common.Address{}, err
	}

	return pk, crypto.PubkeyToAddress(pk.PublicKey), nil
}

//
// Top-level config getters
//

func (c *Config) GetWarpQuorum(blockchainID ids.ID) (WarpQuorum, error) {
	for _, s := range c.DestinationBlockchains {
		if blockchainID.String() == s.BlockchainID {
			return s.warpQuorum, nil
		}
	}
	return WarpQuorum{}, errFailedToGetWarpQuorum
}
