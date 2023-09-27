package block_hash_publisher

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testResult struct {
	isTimeInterval      bool
	blockInterval       int
	timeIntervalSeconds time.Duration
}

func TestConfigValidate(t *testing.T) {
	testCases := []struct {
		name              string
		destinationChains []destinationInfo
		isError           bool
		testResults       []testResult // indexes correspond to destinationChains
	}{
		{
			name: "valid",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "10",
				},
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "10s",
				},
			},
			isError: false,
			testResults: []testResult{
				{
					isTimeInterval: false,
					blockInterval:  10,
				},
				{
					isTimeInterval:      true,
					timeIntervalSeconds: 10 * time.Second,
				},
			},
		},
		{
			name: "invalid chainID",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW7",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "10",
				},
			},
			isError: true,
		},
		{
			name: "invalid interval 1",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "4r",
				},
			},
			isError: true,
		},
		{
			name: "invalid interval 2",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "l",
				},
			},
			isError: true,
		},
		{
			name: "invalid interval 3",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed8635",
					Interval: "",
				},
			},
			isError: true,
		},
		{
			name: "invalid address",
			destinationChains: []destinationInfo{
				{
					ChainID:  "9asUA3QckLh7vGnFQiiUJGPTx8KE4nFtP8c1wTWJuP8XiWW75",
					Address:  "0x50A46AA7b2eCBe2B1AbB7df865B9A87f5eed863",
					Interval: "10",
				},
			},
			isError: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			c := &Config{
				DestinationChains: test.destinationChains,
			}
			err := c.Validate()
			fmt.Println(c)
			if test.isError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				for i, result := range test.testResults {
					require.Equal(t, result.isTimeInterval, c.DestinationChains[i].useTimeInterval)
					if result.isTimeInterval {
						require.Equal(t, result.timeIntervalSeconds, c.DestinationChains[i].timeIntervalSeconds)
					} else {
						require.Equal(t, result.blockInterval, c.DestinationChains[i].blockInterval)
					}
				}
			}
		})
	}
}
