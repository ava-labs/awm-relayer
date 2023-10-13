package block_hash_publisher

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/pkg/errors"
)

type destinationInfo struct {
	ChainID  string `json:"chain-id"`
	Address  string `json:"address"`
	Interval string `json:"interval"`

	useTimeInterval     bool
	blockInterval       uint64
	timeIntervalSeconds uint64
}

type Config struct {
	DestinationChains []destinationInfo `json:"destination-chains"`
}

func (c *Config) Validate() error {
	for i, destinationInfo := range c.DestinationChains {
		// Check if the chainID is valid
		if _, err := ids.FromString(destinationInfo.ChainID); err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid chainID in block hash publisher configuration. Provided ID: %s", destinationInfo.ChainID))
		}

		// Check if the address is valid
		addr := destinationInfo.Address
		if addr == "" {
			return errors.New("empty address in block hash publisher configuration")
		}
		addr = strings.TrimPrefix(addr, "0x")
		_, err := hex.DecodeString(addr)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid address in block hash publisher configuration. Provided address: %s", destinationInfo.Address))
		}

		// Intervals must be either a positive integer, or a positive integer followed by "s"
		interval, isSeconds, err := parseIntervalWithSuffix(destinationInfo.Interval)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid interval in block hash publisher configuration. Provided interval: %s", destinationInfo.Interval))
		}
		if isSeconds {
			c.DestinationChains[i].timeIntervalSeconds = interval
		} else {
			c.DestinationChains[i].blockInterval = interval
		}
		c.DestinationChains[i].useTimeInterval = isSeconds
	}
	return nil
}

func parseIntervalWithSuffix(input string) (uint64, bool, error) {
	// Check if the input string is empty
	if input == "" {
		return 0, false, fmt.Errorf("empty string")
	}

	// Check if the string ends with "s"
	hasSuffix := strings.HasSuffix(input, "s")

	// If it has the "s" suffix, remove it
	if hasSuffix {
		input = input[:len(input)-1]
	}

	// Parse the string as an integer
	intValue, err := strconv.Atoi(input)

	// Check if the parsed value is a positive integer
	if err != nil || intValue < 0 {
		return 0, false, err
	}

	return uint64(intValue), hasSuffix, nil
}
