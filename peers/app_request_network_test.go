// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peers

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
)

func TestCalculateConnectedWeight(t *testing.T) {
	vdr1 := makeValidator(t, 10, 1)
	vdr2 := makeValidator(t, 20, 1)
	vdr3 := makeValidator(t, 30, 2)
	vdrs := []*warp.Validator{&vdr1, &vdr2, &vdr3}
	nodeValidatorIndexMap := map[ids.NodeID]int{
		vdr1.NodeIDs[0]: 0,
		vdr2.NodeIDs[0]: 1,
		vdr3.NodeIDs[0]: 2,
		vdr3.NodeIDs[1]: 2,
	}
	var connectedNodes set.Set[ids.NodeID]
	connectedNodes.Add(vdr1.NodeIDs[0])
	connectedNodes.Add(vdr2.NodeIDs[0])

	// vdr1 and vdr2 are connected, so their weight should be added
	require.Equal(t, 2, connectedNodes.Len())
	connectedWeight := calculateConnectedWeight(vdrs, nodeValidatorIndexMap, connectedNodes)
	require.Equal(t, uint64(30), connectedWeight)

	// Add one of the vdr3's nodeIDs to the connected nodes
	// and confirm that it adds vdr3's weight
	connectedNodes.Add(vdr3.NodeIDs[0])
	require.Equal(t, 3, connectedNodes.Len())
	connectedWeight2 := calculateConnectedWeight(vdrs, nodeValidatorIndexMap, connectedNodes)
	require.Equal(t, uint64(60), connectedWeight2)

	// Add another of vdr3's nodeIDs to the connected nodes
	// and confirm that it's weight isn't double counted
	connectedNodes.Add(vdr3.NodeIDs[1])
	require.Equal(t, 4, connectedNodes.Len())
	connectedWeight3 := calculateConnectedWeight(vdrs, nodeValidatorIndexMap, connectedNodes)
	require.Equal(t, uint64(60), connectedWeight3)
}

func makeValidator(t *testing.T, weight uint64, numNodeIDs int) warp.Validator {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	nodeIDs := make([]ids.NodeID, numNodeIDs)
	for i := 0; i < numNodeIDs; i++ {
		nodeIDs[i] = ids.GenerateTestNodeID()
	}
	return warp.Validator{
		PublicKey:      pk,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
		Weight:         weight,
		NodeIDs:        nodeIDs,
	}
}
