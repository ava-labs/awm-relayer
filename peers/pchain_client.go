// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers/utils"

	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
)

// PChainAPI is a wrapper around the platformvm.Client,
// and provides additional options for the API
// passed in the config.
type PChainAPI struct {
	client  platformvm.Client
	options []rpc.Option
}

func NewPChainAPI(apiConfig *config.APIConfig) *PChainAPI {
	client := platformvm.NewClient(apiConfig.BaseURL)
	options := utils.InitializeOptions(apiConfig)
	return &PChainAPI{
		client:  client,
		options: options,
	}
}

func (p *PChainAPI) GetHeight(ctx context.Context) (uint64, error) {
	return p.client.GetHeight(ctx, p.options...)
}

func (p *PChainAPI) GetProposedHeight(ctx context.Context) (uint64, error) {
	return p.client.GetProposedHeight(ctx, p.options...)
}

func (p *PChainAPI) ExportKey(ctx context.Context, user api.UserPass, address ids.ShortID) (*secp256k1.PrivateKey, error) {
	return p.client.ExportKey(ctx, user, address, p.options...)
}

func (p *PChainAPI) GetBalance(ctx context.Context, addrs []ids.ShortID) (*platformvm.GetBalanceResponse, error) {
	return p.client.GetBalance(ctx, addrs, p.options...)
}

func (p *PChainAPI) ListAddresses(ctx context.Context, user api.UserPass) ([]ids.ShortID, error) {
	return p.client.ListAddresses(ctx, user, p.options...)
}

func (p *PChainAPI) GetUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
) ([][]byte, ids.ShortID, ids.ID, error) {
	return p.client.GetUTXOs(ctx, addrs, limit, startAddress, startUTXOID, p.options...)
}

func (p *PChainAPI) GetAtomicUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain string,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
) ([][]byte, ids.ShortID, ids.ID, error) {
	return p.client.GetAtomicUTXOs(ctx, addrs, sourceChain, limit, startAddress, startUTXOID, p.options...)
}

func (p *PChainAPI) GetSubnet(ctx context.Context, subnetID ids.ID) (platformvm.GetSubnetClientResponse, error) {
	return p.client.GetSubnet(ctx, subnetID, p.options...)
}

func (p *PChainAPI) GetSubnets(ctx context.Context, subnetIDs []ids.ID) ([]platformvm.ClientSubnet, error) {
	return p.client.GetSubnets(ctx, subnetIDs, p.options...)
}

func (p *PChainAPI) GetStakingAssetID(ctx context.Context, subnetID ids.ID) (ids.ID, error) {
	return p.client.GetStakingAssetID(ctx, subnetID, p.options...)
}

func (p *PChainAPI) GetCurrentValidators(ctx context.Context, subnetID ids.ID, nodeIDs []ids.NodeID) ([]platformvm.ClientPermissionlessValidator, error) {
	return p.client.GetCurrentValidators(ctx, subnetID, nodeIDs, p.options...)
}

func (p *PChainAPI) GetL1Validator(ctx context.Context, validationID ids.ID) (platformvm.L1Validator, uint64, error) {
	return p.client.GetL1Validator(ctx, validationID, p.options...)
}

func (p *PChainAPI) GetCurrentSupply(ctx context.Context, subnetID ids.ID) (uint64, uint64, error) {
	return p.client.GetCurrentSupply(ctx, subnetID, p.options...)
}

func (p *PChainAPI) SampleValidators(ctx context.Context, subnetID ids.ID, sampleSize uint16) ([]ids.NodeID, error) {
	return p.client.SampleValidators(ctx, subnetID, sampleSize, p.options...)
}

func (p *PChainAPI) GetBlockchainStatus(ctx context.Context, blockchainID string) (status.BlockchainStatus, error) {
	return p.client.GetBlockchainStatus(ctx, blockchainID, p.options...)
}

func (p *PChainAPI) ValidatedBy(ctx context.Context, blockchainID ids.ID) (ids.ID, error) {
	return p.client.ValidatedBy(ctx, blockchainID, p.options...)
}

func (p *PChainAPI) Validates(ctx context.Context, subnetID ids.ID) ([]ids.ID, error) {
	return p.client.Validates(ctx, subnetID, p.options...)
}

func (p *PChainAPI) GetBlockchains(ctx context.Context) ([]platformvm.APIBlockchain, error) {
	return p.client.GetBlockchains(ctx, p.options...)
}

func (p *PChainAPI) IssueTx(ctx context.Context, tx []byte) (ids.ID, error) {
	return p.client.IssueTx(ctx, tx, p.options...)
}

func (p *PChainAPI) GetTx(ctx context.Context, txID ids.ID) ([]byte, error) {
	return p.client.GetTx(ctx, txID, p.options...)
}

func (p *PChainAPI) GetTxStatus(ctx context.Context, txID ids.ID) (*platformvm.GetTxStatusResponse, error) {
	return p.client.GetTxStatus(ctx, txID, p.options...)
}

func (p *PChainAPI) GetStake(
	ctx context.Context,
	addrs []ids.ShortID,
	validatorsOnly bool,

) (map[ids.ID]uint64, [][]byte, error) {
	return p.client.GetStake(ctx, addrs, validatorsOnly, p.options...)
}

func (p *PChainAPI) GetMinStake(ctx context.Context, subnetID ids.ID) (uint64, uint64, error) {
	return p.client.GetMinStake(ctx, subnetID, p.options...)
}

func (p *PChainAPI) GetTotalStake(ctx context.Context, subnetID ids.ID) (uint64, error) {
	return p.client.GetTotalStake(ctx, subnetID, p.options...)
}

func (p *PChainAPI) GetRewardUTXOs(ctx context.Context, args *api.GetTxArgs) ([][]byte, error) {
	return p.client.GetRewardUTXOs(ctx, args, p.options...)
}

func (p *PChainAPI) GetTimestamp(ctx context.Context) (time.Time, error) {
	return p.client.GetTimestamp(ctx, p.options...)
}

func (p *PChainAPI) GetValidatorsAt(
	ctx context.Context,
	subnetID ids.ID,
	height platformapi.Height,

) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return p.client.GetValidatorsAt(ctx, subnetID, height, p.options...)
}

func (p *PChainAPI) GetBlock(ctx context.Context, blockID ids.ID) ([]byte, error) {
	return p.client.GetBlock(ctx, blockID, p.options...)
}

func (p *PChainAPI) GetBlockByHeight(ctx context.Context, height uint64) ([]byte, error) {
	return p.client.GetBlockByHeight(ctx, height, p.options...)
}

func (p *PChainAPI) GetFeeConfig(ctx context.Context) (*gas.Config, error) {
	return p.client.GetFeeConfig(ctx, p.options...)
}

func (p *PChainAPI) GetFeeState(ctx context.Context) (gas.State, gas.Price, time.Time, error) {
	return p.client.GetFeeState(ctx, p.options...)
}
