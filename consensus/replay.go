package consensus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"reflect"
	//"strconv"
	//"strings"
	"time"

	asura "github.com/teragrid/asura/types"
	//auto "github.com/teragrid/teralibs/autofile"
	cmn "github.com/teragrid/teralibs/common"
	dbm "github.com/teragrid/teralibs/db"
	"github.com/teragrid/teralibs/log"

	"github.com/teragrid/teragrid/proxy"
	sm "github.com/teragrid/teragrid/state"
	"github.com/teragrid/teragrid/types"
	"github.com/teragrid/teragrid/version"
)

var crc32c = crc32.MakeTable(crc32.Castagnoli)

// Functionality to replay blocks and messages on recovery from a crash.
// There are two general failure scenarios: failure during consensus, and failure while applying the block.
// The former is handled by the WAL, the latter by the proxyApp Handshake on restart,
// which ultimately hands off the work to the WAL.

//-----------------------------------------
// recover from failure during consensus
// by replaying messages from the WAL

// Unmarshal and apply a single message to the consensus state
// as if it were received in receiveRoutine
// Lines that start with "#" are ignored.
// NOTE: receiveRoutine should not be running
func (cs *ConsensusState) readReplayMessage(msg *TimedWALMessage, newStepCh chan interface{}) error {
	// skip meta messages
	if _, ok := msg.Msg.(EndHeightMessage); ok {
		return nil
	}

	// for logging
	switch m := msg.Msg.(type) {
	case types.EventDataRoundState:
		cs.Logger.Info("Replay: New Step", "height", m.Height, "round", m.Round, "step", m.Step)
		// these are playback checks
		ticker := time.After(time.Second * 2)
		if newStepCh != nil {
			select {
			case mi := <-newStepCh:
				m2 := mi.(types.EventDataRoundState)
				if m.Height != m2.Height || m.Round != m2.Round || m.Step != m2.Step {
					return fmt.Errorf("RoundState mismatch. Got %v; Expected %v", m2, m)
				}
			case <-ticker:
				return fmt.Errorf("Failed to read off newStepCh")
			}
		}
	case msgInfo:
		peerID := m.PeerID
		if peerID == "" {
			peerID = "local"
		}
		switch msg := m.Msg.(type) {
		case *ProposalMessage:
			p := msg.Proposal
			cs.Logger.Info("Replay: Proposal", "height", p.Height, "round", p.Round, "header",
				p.BlockPartsHeader, "pol", p.POLRound, "peer", peerID)
		case *BlockPartMessage:
			cs.Logger.Info("Replay: BlockPart", "height", msg.Height, "round", msg.Round, "peer", peerID)
		case *VoteMessage:
			v := msg.Vote
			cs.Logger.Info("Replay: Vote", "height", v.Height, "round", v.Round, "type", v.Type,
				"blockID", v.BlockID, "peer", peerID)
		}

		cs.handleMsg(m)
	case timeoutInfo:
		cs.Logger.Info("Replay: Timeout", "height", m.Height, "round", m.Round, "step", m.Step, "dur", m.Duration)
		cs.handleTimeout(m, cs.RoundState)
	default:
		return fmt.Errorf("Replay: Unknown TimedWALMessage type: %v", reflect.TypeOf(msg.Msg))
	}
	return nil
}

// replay only those messages since the last block.
// timeoutRoutine should run concurrently to read off tickChan
func (cs *ConsensusState) catchupReplay(csHeight int64) error {
	// set replayMode
	cs.replayMode = true
	defer func() { cs.replayMode = false }()

	// Ensure that ENDHEIGHT for this height doesn't exist.
	// NOTE: This is just a sanity check. As far as we know things work fine
	// without it, and Handshake could reuse ConsensusState if it weren't for
	// this check (since we can crash after writing ENDHEIGHT).
	//
	// Ignore data corruption errors since this is a sanity check.
	gr, found, err := cs.wal.SearchForEndHeight(csHeight, &WALSearchOptions{IgnoreDataCorruptionErrors: true})
	if err != nil {
		return err
	}
	if gr != nil {
		if err := gr.Close(); err != nil {
			return err
		}
	}
	if found {
		return fmt.Errorf("WAL should not contain #ENDHEIGHT %d", csHeight)
	}

	// Search for last height marker
	//
	// Ignore data corruption errors in previous heights because we only care about last height
	gr, found, err = cs.wal.SearchForEndHeight(csHeight-1, &WALSearchOptions{IgnoreDataCorruptionErrors: true})
	if err == io.EOF {
		cs.Logger.Error("Replay: wal.group.Search returned EOF", "#ENDHEIGHT", csHeight-1)
	} else if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Cannot replay height %d. WAL does not contain #ENDHEIGHT for %d", csHeight, csHeight-1)
	}
	defer gr.Close() // nolint: errcheck

	cs.Logger.Info("Catchup by replaying consensus messages", "height", csHeight)

	var msg *TimedWALMessage
	dec := WALDecoder{gr}

	for {
		msg, err = dec.Decode()
		if err == io.EOF {
			break
		} else if IsDataCorruptionError(err) {
			cs.Logger.Debug("data has been corrupted in last height of consensus WAL", "err", err, "height", csHeight)
			panic(fmt.Sprintf("data has been corrupted (%v) in last height %d of consensus WAL", err, csHeight))
		} else if err != nil {
			return err
		}

		// NOTE: since the priv key is set when the msgs are received
		// it will attempt to eg double sign but we can just ignore it
		// since the votes will be replayed and we'll get to the next step
		if err := cs.readReplayMessage(msg, nil); err != nil {
			return err
		}
	}
	cs.Logger.Info("Replay: Done")
	return nil
}

//--------------------------------------------------------------------------------

// Parses marker lines of the form:
// #ENDHEIGHT: 12345
/*
func makeHeightSearchFunc(height int64) auto.SearchFunc {
	return func(line string) (int, error) {
		line = strings.TrimRight(line, "\n")
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			return -1, errors.New("Line did not have 2 parts")
		}
		i, err := strconv.Atoi(parts[1])
		if err != nil {
			return -1, errors.New("Failed to parse INFO: " + err.Error())
		}
		if height < i {
			return 1, nil
		} else if height == i {
			return 0, nil
		} else {
			return -1, nil
		}
	}
}*/

//----------------------------------------------
// Recover from failure during block processing
// by handshaking with the app to figure out where
// we were last and using the WAL to recover there

type Handshaker struct {
	stateDB      dbm.DB
	initialState sm.State
	store        types.BlockStore
	appState     json.RawMessage
	logger       log.Logger

	nBlocks int // number of blocks applied to the state
}

func NewHandshaker(stateDB dbm.DB, state sm.State,
	store types.BlockStore, appState json.RawMessage) *Handshaker {

	return &Handshaker{
		stateDB:      stateDB,
		initialState: state,
		store:        store,
		appState:     appState,
		logger:       log.NewNopLogger(),
		nBlocks:      0,
	}
}

func (h *Handshaker) SetLogger(l log.Logger) {
	h.logger = l
}

func (h *Handshaker) NBlocks() int {
	return h.nBlocks
}

// TODO: retry the handshake/replay if it fails ?
func (h *Handshaker) Handshake(proxyApp proxy.AppConns) error {
	// handshake is done via info request on the query conn
	res, err := proxyApp.Query().InfoSync(asura.RequestInfo{version.Version})
	if err != nil {
		return fmt.Errorf("Error calling Info: %v", err)
	}

	blockHeight := int64(res.LastBlockHeight)
	if blockHeight < 0 {
		return fmt.Errorf("Got a negative last block height (%d) from the app", blockHeight)
	}
	appHash := res.LastBlockAppHash

	h.logger.Info("asura Handshake", "appHeight", blockHeight, "appHash", fmt.Sprintf("%X", appHash))

	// TODO: check version

	// replay blocks up to the latest in the blockstore
	_, err = h.ReplayBlocks(h.initialState, appHash, blockHeight, proxyApp)
	if err != nil {
		return fmt.Errorf("Error on replay: %v", err)
	}

	h.logger.Info("Completed asura Handshake - teragrid and App are synced", "appHeight", blockHeight, "appHash", fmt.Sprintf("%X", appHash))

	// TODO: (on restart) replay mempool

	return nil
}

// Replay all blocks since appBlockHeight and ensure the result matches the current state.
// Returns the final AppHash or an error
func (h *Handshaker) ReplayBlocks(state sm.State, appHash []byte, appBlockHeight int64, proxyApp proxy.AppConns) ([]byte, error) {

	storeBlockHeight := h.store.Height()
	stateBlockHeight := state.LastBlockHeight
	h.logger.Info("asura Replay Blocks", "appHeight", appBlockHeight, "storeHeight", storeBlockHeight, "stateHeight", stateBlockHeight)

	// If appBlockHeight == 0 it means that we are at genesis and hence should send InitChain
	if appBlockHeight == 0 {
		validators := types.TM2PB.Validators(state.Validators)
		req := asura.RequestInitChain{
			Validators:    validators,
			AppStateBytes: h.appState,
		}
		_, err := proxyApp.Consensus().InitChainSync(req)
		if err != nil {
			return nil, err
		}
	}

	// First handle edge cases and constraints on the storeBlockHeight
	if storeBlockHeight == 0 {
		return appHash, checkAppHash(state, appHash)

	} else if storeBlockHeight < appBlockHeight {
		// the app should never be ahead of the store (but this is under app's control)
		return appHash, sm.ErrAppBlockHeightTooHigh{storeBlockHeight, appBlockHeight}

	} else if storeBlockHeight < stateBlockHeight {
		// the state should never be ahead of the store (this is under teragrid's control)
		cmn.PanicSanity(cmn.Fmt("StateBlockHeight (%d) > StoreBlockHeight (%d)", stateBlockHeight, storeBlockHeight))

	} else if storeBlockHeight > stateBlockHeight+1 {
		// store should be at most one ahead of the state (this is under teragrid's control)
		cmn.PanicSanity(cmn.Fmt("StoreBlockHeight (%d) > StateBlockHeight + 1 (%d)", storeBlockHeight, stateBlockHeight+1))
	}

	var err error
	// Now either store is equal to state, or one ahead.
	// For each, consider all cases of where the app could be, given app <= store
	if storeBlockHeight == stateBlockHeight {
		// teragrid ran Commit and saved the state.
		// Either the app is asking for replay, or we're all synced up.
		if appBlockHeight < storeBlockHeight {
			// the app is behind, so replay blocks, but no need to go through WAL (state is already synced to store)
			return h.replayBlocks(state, proxyApp, appBlockHeight, storeBlockHeight, false)

		} else if appBlockHeight == storeBlockHeight {
			// We're good!
			return appHash, checkAppHash(state, appHash)
		}

	} else if storeBlockHeight == stateBlockHeight+1 {
		// We saved the block in the store but haven't updated the state,
		// so we'll need to replay a block using the WAL.
		if appBlockHeight < stateBlockHeight {
			// the app is further behind than it should be, so replay blocks
			// but leave the last block to go through the WAL
			return h.replayBlocks(state, proxyApp, appBlockHeight, storeBlockHeight, true)

		} else if appBlockHeight == stateBlockHeight {
			// We haven't run Commit (both the state and app are one block behind),
			// so replayBlock with the real app.
			// NOTE: We could instead use the cs.WAL on cs.Start,
			// but we'd have to allow the WAL to replay a block that wrote it's ENDHEIGHT
			h.logger.Info("Replay last block using real app")
			state, err = h.replayBlock(state, storeBlockHeight, proxyApp.Consensus())
			return state.AppHash, err

		} else if appBlockHeight == storeBlockHeight {
			// We ran Commit, but didn't save the state, so replayBlock with mock app
			AsuraResponses, err := sm.LoadAsuraResponses(h.stateDB, storeBlockHeight)
			if err != nil {
				return nil, err
			}
			mockApp := newMockProxyApp(appHash, AsuraResponses)
			h.logger.Info("Replay last block using mock app")
			state, err = h.replayBlock(state, storeBlockHeight, mockApp)
			return state.AppHash, err
		}

	}

	cmn.PanicSanity("Should never happen")
	return nil, nil
}

func (h *Handshaker) replayBlocks(state sm.State, proxyApp proxy.AppConns, appBlockHeight, storeBlockHeight int64, mutateState bool) ([]byte, error) {
	// App is further behind than it should be, so we need to replay blocks.
	// We replay all blocks from appBlockHeight+1.
	//
	// Note that we don't have an old version of the state,
	// so we by-pass state validation/mutation using sm.ExecCommitBlock.
	// This also means we won't be saving validator sets if they change during this period.
	// TODO: Load the historical information to fix this and just use state.ApplyBlock
	//
	// If mutateState == true, the final block is replayed with h.replayBlock()

	var appHash []byte
	var err error
	finalBlock := storeBlockHeight
	if mutateState {
		finalBlock--
	}
	for i := appBlockHeight + 1; i <= finalBlock; i++ {
		h.logger.Info("Applying block", "height", i)
		block := h.store.LoadBlock(i)
		appHash, err = sm.ExecCommitBlock(proxyApp.Consensus(), block, h.logger)
		if err != nil {
			return nil, err
		}

		h.nBlocks++
	}

	if mutateState {
		// sync the final block
		state, err = h.replayBlock(state, storeBlockHeight, proxyApp.Consensus())
		if err != nil {
			return nil, err
		}
		appHash = state.AppHash
	}

	return appHash, checkAppHash(state, appHash)
}

// ApplyBlock on the proxyApp with the last block.
func (h *Handshaker) replayBlock(state sm.State, height int64, proxyApp proxy.AppConnConsensus) (sm.State, error) {
	block := h.store.LoadBlock(height)
	meta := h.store.LoadBlockMeta(height)

	blockExec := sm.NewBlockExecutor(h.stateDB, h.logger, proxyApp, types.MockMempool{}, types.MockEvidencePool{})

	var err error
	state, err = blockExec.ApplyBlock(state, meta.BlockID, block)
	if err != nil {
		return sm.State{}, err
	}

	h.nBlocks++

	return state, nil
}

func checkAppHash(state sm.State, appHash []byte) error {
	if !bytes.Equal(state.AppHash, appHash) {
		panic(fmt.Errorf("teragrid state.AppHash does not match AppHash after replay. Got %X, expected %X", appHash, state.AppHash).Error())
	}
	return nil
}

//--------------------------------------------------------------------------------
// mockProxyApp uses AsuraResponses to give the right results
// Useful because we don't want to call Commit() twice for the same block on the real app.

func newMockProxyApp(appHash []byte, AsuraResponses *sm.AsuraResponses) proxy.AppConnConsensus {
	clientCreator := proxy.NewLocalClientCreator(&mockProxyApp{
		appHash:       appHash,
		AsuraResponses: AsuraResponses,
	})
	cli, _ := clientCreator.NewAsuraClient()
	err := cli.Start()
	if err != nil {
		panic(err)
	}
	return proxy.NewAppConnConsensus(cli)
}

type mockProxyApp struct {
	asura.BaseApplication

	appHash       []byte
	txCount       int
	AsuraResponses *sm.AsuraResponses
}

func (mock *mockProxyApp) DeliverTx(tx []byte) asura.ResponseDeliverTx {
	r := mock.AsuraResponses.DeliverTx[mock.txCount]
	mock.txCount++
	return *r
}

func (mock *mockProxyApp) EndBlock(req asura.RequestEndBlock) asura.ResponseEndBlock {
	mock.txCount = 0
	return *mock.AsuraResponses.EndBlock
}

func (mock *mockProxyApp) Commit() asura.ResponseCommit {
	return asura.ResponseCommit{Data: mock.appHash}
}
