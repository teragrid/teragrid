package core_types

import (
	"encoding/json"
	"strings"
	"time"

	asura "github.com/teragrid/asura/types"
	crypto "github.com/teragrid/go-crypto"
	cmn "github.com/teragrid/teralibs/common"

	"github.com/teragrid/teragrid/p2p"
	"github.com/teragrid/teragrid/state"
	"github.com/teragrid/teragrid/types"
)

type ResultBlockchainInfo struct {
	LastHeight int64              `json:"last_height"`
	BlockMetas []*types.BlockMeta `json:"block_metas"`
}

type ResultGenesis struct {
	Genesis *types.GenesisDoc `json:"genesis"`
}

type ResultBlock struct {
	BlockMeta *types.BlockMeta `json:"block_meta"`
	Block     *types.Block     `json:"block"`
}

type ResultCommit struct {
	// SignedHeader is header and commit, embedded so we only have
	// one level in the json output
	types.SignedHeader
	CanonicalCommit bool `json:"canonical"`
}

type ResultBlockResults struct {
	Height  int64                `json:"height"`
	Results *state.AsuraResponses `json:"results"`
}

// NewResultCommit is a helper to initialize the ResultCommit with
// the embedded struct
func NewResultCommit(header *types.Header, commit *types.Commit,
	canonical bool) *ResultCommit {

	return &ResultCommit{
		SignedHeader: types.SignedHeader{
			Header: header,
			Commit: commit,
		},
		CanonicalCommit: canonical,
	}
}

type SyncInfo struct {
	LatestBlockHash   cmn.HexBytes `json:"latest_block_hash"`
	LatestAppHash     cmn.HexBytes `json:"latest_app_hash"`
	LatestBlockHeight int64        `json:"latest_block_height"`
	LatestBlockTime   time.Time    `json:"latest_block_time"`
	Syncing           bool         `json:"syncing"`
}

type ValidatorInfo struct {
	PubKey      crypto.PubKey `json:"pub_key"`
	VotingPower int64         `json:"voting_power"`
}

type ResultStatus struct {
	NodeInfo      p2p.NodeInfo  `json:"node_info"`
	SyncInfo      SyncInfo      `json:"sync_info"`
	ValidatorInfo ValidatorInfo `json:"validator_info"`
}

func (s *ResultStatus) TxIndexEnabled() bool {
	if s == nil {
		return false
	}
	for _, s := range s.NodeInfo.Other {
		info := strings.Split(s, "=")
		if len(info) == 2 && info[0] == "tx_index" {
			return info[1] == "on"
		}
	}
	return false
}

type ResultNetInfo struct {
	Listening bool     `json:"listening"`
	Listeners []string `json:"listeners"`
	Peers     []Peer   `json:"peers"`
}

type ResultDialSeeds struct {
	Log string `json:"log"`
}

type ResultDialPeers struct {
	Log string `json:"log"`
}

type Peer struct {
	p2p.NodeInfo     `json:"node_info"`
	IsOutbound       bool                 `json:"is_outbound"`
	ConnectionStatus p2p.ConnectionStatus `json:"connection_status"`
}

type ResultValidators struct {
	BlockHeight int64              `json:"block_height"`
	Validators  []*types.Validator `json:"validators"`
}

type ResultDumpConsensusState struct {
	RoundState      json.RawMessage            `json:"round_state"`
	PeerRoundStates map[p2p.ID]json.RawMessage `json:"peer_round_states"`
}

type ResultBroadcastTx struct {
	Code uint32       `json:"code"`
	Data cmn.HexBytes `json:"data"`
	Log  string       `json:"log"`

	Hash cmn.HexBytes `json:"hash"`
}

type ResultBroadcastTxCommit struct {
	CheckTx   asura.ResponseCheckTx   `json:"check_tx"`
	DeliverTx asura.ResponseDeliverTx `json:"deliver_tx"`
	Hash      cmn.HexBytes           `json:"hash"`
	Height    int64                  `json:"height"`
}

type ResultTx struct {
	Hash     cmn.HexBytes           `json:"hash"`
	Height   int64                  `json:"height"`
	Index    uint32                 `json:"index"`
	TxResult asura.ResponseDeliverTx `json:"tx_result"`
	Tx       types.Tx               `json:"tx"`
	Proof    types.TxProof          `json:"proof,omitempty"`
}

type ResultUnconfirmedTxs struct {
	N   int        `json:"n_txs"`
	Txs []types.Tx `json:"txs"`
}

type ResultAsuraInfo struct {
	Response asura.ResponseInfo `json:"response"`
}

type ResultAsuraQuery struct {
	Response asura.ResponseQuery `json:"response"`
}

type ResultUnsafeFlushMempool struct{}

type ResultUnsafeProfile struct{}

type ResultSubscribe struct{}

type ResultUnsubscribe struct{}

type ResultEvent struct {
	Query string            `json:"query"`
	Data  types.TMEventData `json:"data"`
}

type ResultHealth struct{}