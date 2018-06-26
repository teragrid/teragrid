package config

import (
	"testing"

	_ "github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	assert := assert.New(t)

	// set up some defaults
	cfg := DefaultConfig()

	assert.NotNil(cfg.RootDir)
	//	assert.NotNil(cfg.Mempool)
	//	assert.NotNil(cfg.Consensus)

	//	// check the root dir stuff...
	cfg.SetRoot("/foo")
	//	cfg.Genesis = "bar"
	//	cfg.DBPath = "/opt/data"
	//	cfg.Mempool.WalPath = "wal/mem/"

	//	assert.Equal("/foo/bar", cfg.GenesisFile())
	//	assert.Equal("/opt/data", cfg.DBDir())
	//	assert.Equal("/foo/wal/mem", cfg.Mempool.WalDir())

}
