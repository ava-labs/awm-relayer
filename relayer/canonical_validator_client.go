// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"go.uber.org/zap"
)

// CanonicalValidatorClient wraps platformvm.Client and implements validators.State
type CanonicalValidatorClient struct {
	client platformvm.Client
	logger logging.Logger
}

func NewCanonicalValidatorClient(logger logging.Logger, client platformvm.Client) *CanonicalValidatorClient {
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

// Gets the current validator set of the given subnet ID, including the validators' BLS public keys.
// This implementation of GetValidatorSet currently makes two RPC requests, one to get the
// subnet validators, and another to get their BLS public keys. This is necessary in order to enable
// the use of the public APIs (which don't support "GetValidatorsAt") because BLS keys are currently
// only associated with primary network validation periods. If ACP-13 is implementated in the future
// (https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/13-subnet-only-validators.md), it may
// become possible to reduce this to a single RPC request that returns both the subnet validators
// as well as their BLS public keys.
func (v *CanonicalValidatorClient) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Get the current subnet validators. These validators are not expected to include
	// BLS signing information given that addPermissionlessValidatorTx is only used to
	// add primary network validators.
	subnetVdrs, err := v.client.GetCurrentValidators(ctx, subnetID, nil)
	if err != nil {
		return nil, err
	}

	// Look up the primary network validators of the NodeIDs validating the subnet
	// in order to get their BLS keys.
	res := make(map[ids.NodeID]*validators.GetValidatorOutput, len(subnetVdrs))
	subnetNodeIDs := make([]ids.NodeID, 0, len(subnetVdrs))
	for _, subnetVdr := range subnetVdrs {
		subnetNodeIDs = append(subnetNodeIDs, subnetVdr.NodeID)
		res[subnetVdr.NodeID] = &validators.GetValidatorOutput{
			NodeID: subnetVdr.NodeID,
			Weight: subnetVdr.Weight,
		}
	}
	primaryVdrs, err := v.client.GetCurrentValidators(ctx, ids.Empty, subnetNodeIDs)
	if err != nil {
		return nil, err
	}

	// Set the BLS keys of the result.
	for _, primaryVdr := range primaryVdrs {
		vdr, ok := res[primaryVdr.NodeID]
		if !ok {
			v.logger.Warn(
				"Missing primary network validator for subnet validator",
				zap.String("subnetID", subnetID.String()),
				zap.String("nodeID", primaryVdr.NodeID.String()))
			continue
		}
		vdr.PublicKey = primaryVdr.Signer.Key()
	}

	return res, nil
}
