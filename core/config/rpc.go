package config

import (
	"errors"
	"path/filepath"
	"time"
)

// RPCConfig

// RPCConfig defines the configuration options for the Dgrid RPC server
type RPCConfig struct {
	RootDir string `mapstructure:"home"`

	// TCP or UNIX socket address for the RPC server to listen on
	ListenAddress string `mapstructure:"laddr"`

	// A list of origins a cross-domain request can be executed from.
	// If the special '*' value is present in the list, all origins will be allowed.
	// An origin may contain a wildcard (*) to replace 0 or more characters (i.e.: http://*.domain.com).
	// Only one wildcard can be used per origin.
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins"`

	// A list of methods the client is allowed to use with cross-domain requests.
	CORSAllowedMethods []string `mapstructure:"cors_allowed_methods"`

	// A list of non simple headers the client is allowed to use with cross-domain requests.
	CORSAllowedHeaders []string `mapstructure:"cors_allowed_headers"`

	// TCP or UNIX socket address for the gRPC server to listen on
	// NOTE: This server only supports /broadcast_tx_commit
	GRPCListenAddress string `mapstructure:"grpc_laddr"`

	// Maximum number of simultaneous connections.
	// Does not include RPC (HTTP&WebSocket) connections. See max_open_connections
	// If you want to accept a larger number than the default, make sure
	// you increase your OS limits.
	// 0 - unlimited.
	GRPCMaxOpenConnections int `mapstructure:"grpc_max_open_connections"`

	// Activate unsafe RPC commands like /dial_persistent_peers and /unsafe_flush_storage
	Unsafe bool `mapstructure:"unsafe"`

	// Maximum number of simultaneous connections (including WebSocket).
	// Does not include gRPC connections. See grpc_max_open_connections
	// If you want to accept a larger number than the default, make sure
	// you increase your OS limits.
	// 0 - unlimited.
	// Should be < {ulimit -Sn} - {MaxNumInboundPeers} - {MaxNumOutboundPeers} - {N of wal, db and other open files}
	// 1024 - 40 - 10 - 50 = 924 = ~900
	MaxOpenConnections int `mapstructure:"max_open_connections"`

	// Maximum number of unique clientIDs that can /subscribe
	// If you're using /broadcast_tx_commit, set to the estimated maximum number
	// of broadcast_tx_commit calls per block.
	MaxSubscriptionClients int `mapstructure:"max_subscription_clients"`

	// Maximum number of unique queries a given client can /subscribe to
	// If you're using GRPC (or Local RPC client) and /broadcast_tx_commit, set
	// to the estimated maximum number of broadcast_tx_commit calls per block.
	MaxSubscriptionsPerClient int `mapstructure:"max_subscriptions_per_client"`

	// How long to wait for a tx to be committed during /broadcast_tx_commit
	// WARNING: Using a value larger than 10s will result in increasing the
	// global HTTP write timeout, which applies to all connections and endpoints.
	TimeoutBroadcastTxCommit time.Duration `mapstructure:"timeout_broadcast_tx_commit"`

	// The name of a file containing certificate that is used to create the HTTPS server.
	//
	// If the certificate is signed by a certificate authority,
	// the certFile should be the concatenation of the server's certificate, any intermediates,
	// and the CA's certificate.
	//
	// NOTE: both tls_cert_file and tls_key_file must be present for Dgrid to create HTTPS server. Otherwise, HTTP server is run.
	TLSCertFile string `mapstructure:"tls_cert_file"`

	// The name of a file containing matching private key that is used to create the HTTPS server.
	//
	// NOTE: both tls_cert_file and tls_key_file must be present for Dgrid to create HTTPS server. Otherwise, HTTP server is run.
	TLSKeyFile string `mapstructure:"tls_key_file"`
}

// Default returns a default configuration for the RPC server
func (cfg *RPCConfig) Default() *Config {
	return &RPCConfig{
		ListenAddress:          "tcp://0.0.0.0:26657",
		CORSAllowedOrigins:     []string{},
		CORSAllowedMethods:     []string{"HEAD", "GET", "POST"},
		CORSAllowedHeaders:     []string{"Origin", "Accept", "Content-Type", "X-Requested-With", "X-Server-Time"},
		GRPCListenAddress:      "",
		GRPCMaxOpenConnections: 900,

		Unsafe:             false,
		MaxOpenConnections: 900,

		MaxSubscriptionClients:    100,
		MaxSubscriptionsPerClient: 5,
		TimeoutBroadcastTxCommit:  10 * time.Second,

		TLSCertFile: "",
		TLSKeyFile:  "",
	}
}

// Validate performs basic validation (checking param bounds, etc.) and
// returns an error if any check fails.
func (cfg *RPCConfig) Validate() error {
	if cfg.GRPCMaxOpenConnections < 0 {
		return errors.New("grpc_max_open_connections can't be negative")
	}
	if cfg.MaxOpenConnections < 0 {
		return errors.New("max_open_connections can't be negative")
	}
	if cfg.MaxSubscriptionClients < 0 {
		return errors.New("max_subscription_clients can't be negative")
	}
	if cfg.MaxSubscriptionsPerClient < 0 {
		return errors.New("max_subscriptions_per_client can't be negative")
	}
	if cfg.TimeoutBroadcastTxCommit < 0 {
		return errors.New("timeout_broadcast_tx_commit can't be negative")
	}
	return nil
}

// IsCorsEnabled returns true if cross-origin resource sharing is enabled.
func (cfg *RPCConfig) IsCorsEnabled() bool {
	return len(cfg.CORSAllowedOrigins) != 0
}

// KeyFile returns the absolute path to the file storing keys
func (cfg RPCConfig) KeyFile() string {
	return Rootify(filepath.Join(defaultConfigDir, cfg.TLSKeyFile), cfg.RootDir)
}

// CertFile returns the absolute path to the certificate file
func (cfg RPCConfig) CertFile() string {
	return Rootify(filepath.Join(defaultConfigDir, cfg.TLSCertFile), cfg.RootDir)
}

// IsTLSEnabled checks if TLS is enabled or not
func (cfg RPCConfig) IsTLSEnabled() bool {
	return cfg.TLSCertFile != "" && cfg.TLSKeyFile != ""
}
