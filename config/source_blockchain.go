package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
)

// Source blockchain configuration.
// Specifies how to connect to and listen for messages on the source blockchain.
// Specifies the message protocols supported by the relayer for this blockchain.
// Specifies the supported source addresses, and destination blockchains and addresses.
// Specifies the height from which to start processing historical blocks.
type SourceBlockchain struct {
	SubnetID                          string                           `mapstructure:"subnet-id" json:"subnet-id"`
	BlockchainID                      string                           `mapstructure:"blockchain-id" json:"blockchain-id"` //nolint:lll
	VM                                string                           `mapstructure:"vm" json:"vm"`
	RPCEndpoint                       APIConfig                        `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`                                                   //nolint:lll
	WSEndpoint                        APIConfig                        `mapstructure:"ws-endpoint" json:"ws-endpoint"`                                                     //nolint:lll
	MessageContracts                  map[string]MessageProtocolConfig `mapstructure:"message-contracts" json:"message-contracts"`                                         //nolint:lll
	SupportedDestinations             []*SupportedDestination          `mapstructure:"supported-destinations" json:"supported-destinations"`                               //nolint:lll
	ProcessHistoricalBlocksFromHeight uint64                           `mapstructure:"process-historical-blocks-from-height" json:"process-historical-blocks-from-height"` //nolint:lll
	AllowedOriginSenderAddresses      []string                         `mapstructure:"allowed-origin-sender-addresses" json:"allowed-origin-sender-addresses"`             //nolint:lll
	WarpAPIEndpoint                   APIConfig                        `mapstructure:"warp-api-endpoint" json:"warp-api-endpoint"`                                         //nolint:lll

	// convenience fields to access parsed data after initialization
	subnetID                     ids.ID
	blockchainID                 ids.ID
	allowedOriginSenderAddresses []common.Address
	useAppRequestNetwork         bool
}

// Validates the source subnet configuration, including verifying that the supported destinations are present in
// destinationBlockchainIDs. Does not modify the public fields as derived from the configuration passed to the
// application, but does initialize private fields available through getters.
func (s *SourceBlockchain) Validate(destinationBlockchainIDs *set.Set[string]) error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in source subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.BlockchainID); err != nil {
		return fmt.Errorf("invalid blockchainID in source subnet configuration. Provided ID: %s", s.BlockchainID)
	}
	if err := s.RPCEndpoint.Validate(); err != nil {
		return fmt.Errorf("invalid rpc-endpoint in source subnet configuration: %w", err)
	}
	if err := s.WSEndpoint.Validate(); err != nil {
		return fmt.Errorf("invalid ws-endpoint in source subnet configuration: %w", err)
	}
	// The Warp API endpoint is optional. If omitted, signatures are fetched from validators via app request.
	if s.WarpAPIEndpoint.BaseURL != "" {
		if err := s.WarpAPIEndpoint.Validate(); err != nil {
			return fmt.Errorf("invalid warp-api-endpoint in source subnet configuration: %w", err)
		}
	} else {
		s.useAppRequestNetwork = true
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
			return fmt.Errorf(
				"configured source subnet %s has a supported destination blockchain ID %s that is not configured as a destination blockchain", //nolint:lll
				s.SubnetID,
				blockchainID)
		}
		dest.blockchainID = blockchainID
		for _, addressStr := range dest.Addresses {
			if !common.IsHexAddress(addressStr) {
				return fmt.Errorf(
					"invalid allowed destination address in source blockchain configuration: %s",
					addressStr,
				)
			}
			address := common.HexToAddress(addressStr)
			if address == utils.ZeroAddress {
				return fmt.Errorf(
					"invalid allowed destination address in source blockchain configuration: %s",
					addressStr,
				)
			}
			dest.addresses = append(dest.addresses, address)
		}
	}

	// Validate and store the allowed origin source addresses
	allowedOriginSenderAddresses := make([]common.Address, len(s.AllowedOriginSenderAddresses))
	for i, addressStr := range s.AllowedOriginSenderAddresses {
		if !common.IsHexAddress(addressStr) {
			return fmt.Errorf(
				"invalid allowed origin sender address in source blockchain configuration: %s",
				addressStr,
			)
		}
		address := common.HexToAddress(addressStr)
		if address == utils.ZeroAddress {
			return fmt.Errorf(
				"invalid allowed origin sender address in source blockchain configuration: %s",
				addressStr,
			)
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

func (s *SourceBlockchain) UseAppRequestNetwork() bool {
	return s.useAppRequestNetwork
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

// The generic configuration for a message protocol.
type MessageProtocolConfig struct {
	MessageFormat string                 `mapstructure:"message-format" json:"message-format"`
	Settings      map[string]interface{} `mapstructure:"settings" json:"settings"`
}
