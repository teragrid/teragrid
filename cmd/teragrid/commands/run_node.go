package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	cfg "github.com/teragrid/teragrid/config"
	nm "github.com/teragrid/teragrid/node"
	cmn "github.com/teragrid/teralibs/common"
)

// AddNodeFlags exposes some common configuration options on the command-line
// These are exposed for convenience of commands embedding a tendermint node
func AddNodeFlags(cmd *cobra.Command) {
	config := mainConfig.ChainConfigs[0]
	// bind flags
	cmd.Flags().String("moniker", config.Moniker, "Node Name")

	// priv val flags
	cmd.Flags().String("priv_validator_laddr", config.PrivValidatorListenAddr, "Socket address to listen on for connections from external priv_validator process")

	// node flags
	cmd.Flags().Bool("fast_sync", config.FastSync, "Fast blockchain syncing")

	// Asura flags
	cmd.Flags().String("proxy_app", config.ProxyApp, "Proxy app address, or 'nilapp' or 'kvstore' for local testing.")
	cmd.Flags().String("asura", config.Asura, "Specify Asura transport (socket | grpc)")

	// rpc flags
	cmd.Flags().String("rpc.laddr", config.RPC.ListenAddress, "RPC listen address. Port required")
	cmd.Flags().String("rpc.grpc_laddr", config.RPC.GRPCListenAddress, "GRPC listen address (BroadcastTx only). Port required")
	cmd.Flags().Bool("rpc.unsafe", config.RPC.Unsafe, "Enabled unsafe rpc methods")

	// p2p flags
	cmd.Flags().String("p2p.laddr", config.P2P.ListenAddress, "Node listen address. (0.0.0.0:0 means any interface, any port)")
	cmd.Flags().String("p2p.seeds", config.P2P.Seeds, "Comma-delimited ID@host:port seed nodes")
	cmd.Flags().String("p2p.persistent_peers", config.P2P.PersistentPeers, "Comma-delimited ID@host:port persistent peers")
	cmd.Flags().Bool("p2p.skip_upnp", config.P2P.SkipUPNP, "Skip UPNP configuration")
	cmd.Flags().Bool("p2p.pex", config.P2P.PexReactor, "Enable/disable Peer-Exchange")
	cmd.Flags().Bool("p2p.seed_mode", config.P2P.SeedMode, "Enable/disable seed mode")
	cmd.Flags().String("p2p.private_peer_ids", config.P2P.PrivatePeerIDs, "Comma-delimited private peer IDs")

	// consensus flags
	cmd.Flags().Bool("consensus.create_empty_blocks", config.Consensus.CreateEmptyBlocks, "Set this to false to only produce blocks when there are txs or when the AppHash changes")
}

// NewRunNodeCmd returns the command that allows the CLI to start a node.
// It can be used with a custom PrivValidator and in-process Asura application.
func NewRunNodeCmd(nodeProvider nm.NodeProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Run the tendermint node",
		RunE: func(cmd *cobra.Command, args []string) error {
			//runOneNode(nodeProvider)
			runNodes(nodeProvider)
			return nil
		},
	}
	AddNodeFlags(cmd)
	return cmd
}

func runOneNode(nodeProvider nm.NodeProvider) error {
	// Create & start node
	config := mainConfig.ChainConfigs[0]
	newLogger := logger.With("chain", config.ChainID())

	newLogger.Info("Run one node: Start with config")
	n, err := nodeProvider(config, newLogger)
	if err != nil {
		return fmt.Errorf("Failed to create node: %v", err)
	}

	if err := n.Start(); err != nil {
		return fmt.Errorf("Failed to start node: %v", err)
	}
	newLogger.Info("Started node", "nodeInfo", n.Switch().NodeInfo())

	// Trap signal, run forever.
	n.RunForever()
	newLogger.Info("runOneNode Finish")
	return nil
}

func runNodeWithConfig(nodeProvider nm.NodeProvider, config *cfg.ChainConfig) (*nm.Node, error) {

	newLogger := logger.With("chain", config.ChainID())
	newLogger.Info("runNodeWithConfig " + config.ChainID())

	// Create & start node
	n, err := nodeProvider(config, newLogger)
	if err != nil {
		return nil, fmt.Errorf("Failed to create node: %v", err)
	}

	if err := n.Start(); err != nil {
		//return fmt.Errorf("Failed to start node: %v", err)
	}
	newLogger.Info("Started node", "nodeInfo", n.Switch().NodeInfo())
	fmt.Println("Create node finish" + config.ChainID())
	// Trap signal, run forever.
	//n.RunForever()
	return n, nil
}

func runNodes(nodeProvider nm.NodeProvider) {
	logger.Info("Run multi-node")
	var nodes []*nm.Node
	nodes = make([]*nm.Node, len(mainConfig.ChainConfigs))
	for idx, configLoop := range mainConfig.ChainConfigs {
		config := configLoop
		//fmt.Println("================================== Create node " + config.ChainID())
		logger.Info("================================== Create node " + config.ChainID())
		nodes[idx], _ = runNodeWithConfig(nodeProvider, config)
	}
	cmn.TrapSignal(func() {
		for _, n := range nodes {
			n.Stop()
		}
	})
	return
}

/*
// NewRunNodeCmd returns the command that allows the CLI to start a node.
// It can be used with a custom PrivValidator and in-process Asura application.
func NewRunNodeCmd(nodeProvider nm.NodeProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Run the tendermint node",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create & start node
			n, err := nodeProvider(config, logger)
			if err != nil {
				return fmt.Errorf("Failed to create node: %v", err)
			}

			if err := n.Start(); err != nil {
				return fmt.Errorf("Failed to start node: %v", err)
			}
			logger.Info("Started node", "nodeInfo", n.Switch().NodeInfo())

			// Trap signal, run forever.
			n.RunForever()

			return nil
		},
	}

	AddNodeFlags(cmd)
	return cmd
}
*/