package config

import (
	"encoding/hex"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/common"
)

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
