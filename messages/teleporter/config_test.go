package teleporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	testCases := []struct {
		name          string
		rewardAddress string
		isError       bool
	}{
		{
			name:          "valid",
			rewardAddress: "0x27aE10273D17Cd7e80de8580A51f476960626e5f",
			isError:       false,
		},
		{
			name:          "invalid",
			rewardAddress: "0x27aE10273D17Cd7e80de8580A51f476960626e5",
			isError:       true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			c := &Config{
				RewardAddress: test.rewardAddress,
			}
			err := c.Validate()
			if test.isError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
