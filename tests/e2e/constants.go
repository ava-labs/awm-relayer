package tests

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/awm-relayer/config"
)

var testConfig = config.Config{
	NetworkID:         constants.UnitTestID,
	EncryptConnection: false,
}
