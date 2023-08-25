// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

// CanonicalValidatorClient wraps platformvm.Client and implements validators.State
type CanonicalValidatorClient struct {
	client platformvm.Client
}

func NewCanonicalValidatorClient(client platformvm.Client) *CanonicalValidatorClient {
	return &CanonicalValidatorClient{
		client: client,
	}
}

func (v *CanonicalValidatorClient) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return v.client.GetHeight(ctx)
}

func (v *CanonicalValidatorClient) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.client.GetHeight(ctx)
}

func (v *CanonicalValidatorClient) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return v.client.ValidatedBy(ctx, chainID)
}

func (v *CanonicalValidatorClient) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return v.client.GetValidatorsAt(ctx, subnetID, height)
}
