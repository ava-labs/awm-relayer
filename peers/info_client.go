// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/awm-relayer/config"
	"github.com/ava-labs/awm-relayer/peers/utils"
)

// InfoAPI is a wrapper around the info.Client,
// and provides additional options for the API
// passed in the config.
type InfoAPI struct {
	client  info.Client
	options []rpc.Option
}

func NewInfoAPI(apiConfig *config.APIConfig) (*InfoAPI, error) {
	client := info.NewClient(apiConfig.BaseURL)
	options := utils.InitializeOptions(apiConfig)
	return &InfoAPI{
		client:  client,
		options: options,
	}, nil
}

func (i *InfoAPI) GetBlockchainID(ctx context.Context, alias string) (ids.ID, error) {
	return i.client.GetBlockchainID(ctx, alias, i.options...)
}

func (i *InfoAPI) GetNetworkID(ctx context.Context) (uint32, error) {
	return i.client.GetNetworkID(ctx, i.options...)
}

func (i *InfoAPI) GetNetworkName(ctx context.Context) (string, error) {
	return i.client.GetNetworkName(ctx, i.options...)
}

func (i *InfoAPI) GetNodeID(ctx context.Context) (ids.NodeID, *signer.ProofOfPossession, error) {
	return i.client.GetNodeID(ctx, i.options...)
}

func (i *InfoAPI) GetNodeIP(ctx context.Context) (string, error) {
	addrPort, err := i.client.GetNodeIP(ctx, i.options...)
	return addrPort.Addr().String(), err
}

func (i *InfoAPI) GetNodeVersion(ctx context.Context) (*info.GetNodeVersionReply, error) {
	return i.client.GetNodeVersion(ctx, i.options...)
}

func (i *InfoAPI) GetTxFee(ctx context.Context) (*info.GetTxFeeResponse, error) {
	return i.client.GetTxFee(ctx, i.options...)
}

func (i *InfoAPI) GetVMs(ctx context.Context) (map[ids.ID][]string, error) {
	return i.client.GetVMs(ctx, i.options...)
}

func (i *InfoAPI) IsBootstrapped(ctx context.Context, chainID string) (bool, error) {
	return i.client.IsBootstrapped(ctx, chainID, i.options...)
}

func (i *InfoAPI) Peers(ctx context.Context) ([]info.Peer, error) {
	return i.client.Peers(ctx, i.options...)
}

func (i *InfoAPI) Uptime(ctx context.Context, subnetID ids.ID) (*info.UptimeResponse, error) {
	return i.client.Uptime(ctx, subnetID, i.options...)
}
