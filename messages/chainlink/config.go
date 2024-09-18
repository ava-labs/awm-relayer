package chainlink

import (
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
)

type RawConfig struct {
	AggregatorsToReplicas map[string]string `json:"aggregators-to-replicas"`
	MaxFilterAdresses     string            `json:"max-filter-addresses"`
}

type Config struct {
	AggregatorsToReplicas map[common.Address]common.Address
	MaxFilterAdresses     uint64
}

func (c *RawConfig) Parse() (*Config, error) {
	maxFilterAdresses, err := strconv.ParseUint(c.MaxFilterAdresses, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid integer for max-filter-addresses cannot be parsed to uint64: %s", c.MaxFilterAdresses)
	}
	aggregatorToReplicas := make(map[common.Address]common.Address, 0)
	for aggregator, replica := range c.AggregatorsToReplicas {
		if !common.IsHexAddress(aggregator) {
			return nil, fmt.Errorf("invalid price feed aggregator address for EVM source subnet: %s", aggregator)
		}
		if !common.IsHexAddress(replica) {
			return nil, fmt.Errorf("invalid price feed replica address for EVM source subnet: %s", replica)
		}
		aggregator, replica := common.HexToAddress(aggregator), common.HexToAddress(replica)
		if _, ok := aggregatorToReplicas[aggregator]; ok {
			return nil, fmt.Errorf("duplicate aggregator entry for %s", aggregator)
		}
		aggregatorToReplicas[aggregator] = replica
	}
	config := Config{
		AggregatorsToReplicas: aggregatorToReplicas,
		MaxFilterAdresses:     maxFilterAdresses,
	}
	return &config, nil
}
