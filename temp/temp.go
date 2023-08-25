// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package temp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"golang.org/x/exp/maps"
)

// Keep this function here until we're able to get a validators.State object from the P-Chain API.
// Then, we can call warp.GetCanonicalValidatorSet
func GetCanonicalValidatorSet(vdrSet []platformvm.ClientPermissionlessValidator) ([]*warp.Validator, uint64, error) {
	var (
		vdrs        = make(map[string]*warp.Validator, len(vdrSet))
		totalWeight uint64
		err         error
	)
	for _, vdr := range vdrSet {
		totalWeight, err = math.Add64(totalWeight, vdr.Weight)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %s", warp.ErrWeightOverflow, err)
		}
		pk := vdr.Signer.Key()
		if pk == nil {
			continue
		}
		pkBytes := pk.Serialize()
		uniqueVdr, ok := vdrs[string(pkBytes)]
		if !ok {
			uniqueVdr = &warp.Validator{
				PublicKey:      pk,
				PublicKeyBytes: pkBytes,
			}
			vdrs[string(pkBytes)] = uniqueVdr
		}
		uniqueVdr.Weight += vdr.Weight // Impossible to overflow here
		uniqueVdr.NodeIDs = append(uniqueVdr.NodeIDs, vdr.NodeID)

	}

	// Sort validators by public key
	vdrList := maps.Values(vdrs)
	utils.Sort(vdrList)
	return vdrList, totalWeight, nil
}
