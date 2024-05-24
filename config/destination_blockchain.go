package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/awm-relayer/ethclient"
	"github.com/ava-labs/awm-relayer/utils"
	"github.com/ethereum/go-ethereum/crypto"
)

// Destination blockchain configuration. Specifies how to connect to and issue transactions on the desination blockchain.
type DestinationBlockchain struct {
	SubnetID          string    `mapstructure:"subnet-id" json:"subnet-id"`
	BlockchainID      string    `mapstructure:"blockchain-id" json:"blockchain-id"`
	VM                string    `mapstructure:"vm" json:"vm"`
	RPCEndpoint       APIConfig `mapstructure:"rpc-endpoint" json:"rpc-endpoint"`
	KMSKeyID          string    `mapstructure:"kms-key-id" json:"kms-key-id"`
	KMSAWSRegion      string    `mapstructure:"kms-aws-region" json:"kms-aws-region"`
	AccountPrivateKey string    `mapstructure:"account-private-key" json:"account-private-key"`

	// Fetched from the chain after startup
	warpQuorum WarpQuorum

	// convenience fields to access parsed data after initialization
	subnetID     ids.ID
	blockchainID ids.ID
}

// Validatees the destination subnet configuration
func (s *DestinationBlockchain) Validate() error {
	if _, err := ids.FromString(s.SubnetID); err != nil {
		return fmt.Errorf("invalid subnetID in destination subnet configuration. Provided ID: %s", s.SubnetID)
	}
	if _, err := ids.FromString(s.BlockchainID); err != nil {
		return fmt.Errorf("invalid blockchainID in destination subnet configuration. Provided ID: %s", s.BlockchainID)
	}
	if err := s.RPCEndpoint.Validate(); err != nil {
		return fmt.Errorf("invalid rpc-endpoint in destination subnet configuration: %w", err)
	}
	if s.KMSKeyID != "" {
		if s.KMSAWSRegion == "" {
			return errors.New("KMS key ID provided without an AWS region")
		}
		if s.AccountPrivateKey != "" {
			return errors.New("only one of account private key or KMS key ID can be provided")
		}
	} else {
		if _, err := crypto.HexToECDSA(utils.SanitizeHexString(s.AccountPrivateKey)); err != nil {
			return utils.ErrInvalidPrivateKeyHex
		}
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

	client, err := ethclient.DialWithConfig(context.Background(), s.RPCEndpoint.BaseURL, s.RPCEndpoint.HTTPHeaders, s.RPCEndpoint.QueryParams)
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

// Warp Quorum configuration, fetched from the chain config
type WarpQuorum struct {
	QuorumNumerator   uint64
	QuorumDenominator uint64
}
