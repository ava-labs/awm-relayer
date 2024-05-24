package config

import (
	"bytes"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	_ "embed"
)

//go:embed test_files/config.json
var configFile string

func TestBuildConfig(t *testing.T) {
	v := viper.New()
	buf := bytes.NewBufferString(configFile)
	v.SetConfigType("json")
	require.NoError(t, v.ReadConfig(buf))
	cfg, over, err := BuildConfig(v)
	require.NoError(t, err)
	require.False(t, over)
	require.Equal(t, defaultLogLevel, cfg.LogLevel)
	require.Equal(t, defaultStorageLocation, cfg.StorageLocation)
	require.Equal(t, defaultAPIPort, cfg.APIPort)
	require.Equal(t, defaultMetricsPort, cfg.MetricsPort)
	require.Equal(t, defaultIntervalSeconds, cfg.DBWriteIntervalSeconds)
	require.Equal(t, &APIConfig{
		BaseURL: "https://api.avax-test.network",
	}, cfg.PChainAPI)
	require.Equal(t, &APIConfig{
		BaseURL: "https://api.avax-test.network",
	}, cfg.InfoAPI)

	require.Len(t, cfg.SourceBlockchains, 1)
	require.Equal(t, &SourceBlockchain{
		SubnetID:     "11111111111111111111111111111111LpoYY",
		BlockchainID: "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
		VM:           "evm",
		RPCEndpoint: APIConfig{
			BaseURL: "https://api.avax-test.network/ext/bc/C/rpc",
		},
		WSEndpoint: APIConfig{
			BaseURL: "wss://api.avax-test.network/ext/bc/C/ws",
		},
		MessageContracts: map[string]MessageProtocolConfig{
			"0x253b2784c75e510dd0ff1da844684a1ac0aa5fcf": {
				MessageFormat: "teleporter",
				Settings: map[string]interface{}{
					"reward-address": "0x5072...",
				},
			},
		},
	}, cfg.SourceBlockchains[0])

	require.Len(t, cfg.DestinationBlockchains, 1)
	require.Equal(t, &DestinationBlockchain{
		SubnetID:     "7WtoAMPhrmh5KosDUsFL9yTcvw7YSxiKHPpdfs4JsgW47oZT5",
		BlockchainID: "2D8RG4UpSXbPbvPCAWppNJyqTG2i2CAXSkTgmTBBvs7GKNZjsY",
		RPCEndpoint: APIConfig{
			BaseURL: "https://subnets.avax.network/dispatch/testnet/rpc",
		},
		VM:                "evm",
		AccountPrivateKey: "f4a2b939592564feb35ab10a8e04f6f2fe0943579fb3c9c33505298978b74893",
	}, cfg.DestinationBlockchains[0])
}
