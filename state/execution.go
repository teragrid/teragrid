package state

import (
	"fmt"

	fail "github.com/ebuchman/fail-test"
	asura "github.com/teragrid/asura/types"
	crypto "github.com/teragrid/go-crypto"
	"github.com/teragrid/teragrid/proxy"
	"github.com/teragrid/teragrid/types"
	dbm "github.com/teragrid/teralibs/db"
	"github.com/teragrid/teralibs/log"
)

//-----------------------------------------------------------------------------
// BlockExecutor handles block execution and state updates.
// It exposes ApplyBlock(), which validates & executes the block, updates state w/ asura responses,
// then commits and updates the mempool atomically, then saves state.

// BlockExecutor provides the context and accessories for properly executing a block.
type BlockExecutor struct {
	// save state, validators, consensus params, asura responses here
	db dbm.DB

	// execute the app against this
	proxyApp proxy.AppConnConsensus

	// events
	eventBus types.BlockEventPublisher

	// update these with block results after commit
	mempool types.Mempool
	evpool  types.EvidencePool

	logger log.Logger
}

// NewBlockExecutor returns a new BlockExecutor with a NopEventBus.
// Call SetEventBus to provide one.
func NewBlockExecutor(db dbm.DB, logger log.Logger, proxyApp proxy.AppConnConsensus,
	mempool types.Mempool, evpool types.EvidencePool) *BlockExecutor {
	return &BlockExecutor{
		db:       db,
		proxyApp: proxyApp,
		eventBus: types.NopEventBus{},
		mempool:  mempool,
		evpool:   evpool,
		logger:   logger,
	}
}

// SetEventBus - sets the event bus for publishing block related events.
// If not called, it defaults to types.NopEventBus.
func (blockExec *BlockExecutor) SetEventBus(eventBus types.BlockEventPublisher) {
	blockExec.eventBus = eventBus
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func (blockExec *BlockExecutor) ValidateBlock(s State, block *types.Block) error {
	return validateBlock(blockExec.db, s, block)
}

// ApplyBlock validates the block against the state, executes it against the app,
// fires the relevant events, commits the app, and saves the new state and responses.
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockID to avoid recomputing the parts hash.
func (blockExec *BlockExecutor) ApplyBlock(s State, blockID types.BlockID, block *types.Block) (State, error) {

	if err := blockExec.ValidateBlock(s, block); err != nil {
		return s, ErrInvalidBlock(err)
	}

	AsuraResponses, err := execBlockOnProxyApp(blockExec.logger, blockExec.proxyApp, block)
	if err != nil {
		return s, ErrProxyAppConn(err)
	}

	fail.Fail() // XXX

	// save the results before we commit
	saveAsuraResponses(blockExec.db, block.Height, AsuraResponses)

	fail.Fail() // XXX

	// update the state with the block and responses
	s, err = updateState(s, blockID, block.Header, AsuraResponses)
	if err != nil {
		return s, fmt.Errorf("Commit failed for application: %v", err)
	}

	// lock mempool, commit state, update mempoool
	appHash, err := blockExec.Commit(block)
	if err != nil {
		return s, fmt.Errorf("Commit failed for application: %v", err)
	}

	fail.Fail() // XXX

	// update the app hash and save the state
	s.AppHash = appHash
	SaveState(blockExec.db, s)

	fail.Fail() // XXX

	// Update evpool now that state is saved
	// TODO: handle the crash/recover scenario
	// ie. (may need to call Update for last block)
	blockExec.evpool.Update(block)

	// events are fired after everything else
	// NOTE: if we crash between Commit and Save, events wont be fired during replay
	fireEvents(blockExec.logger, blockExec.eventBus, block, AsuraResponses)

	return s, nil
}

// Commit locks the mempool, runs the asura Commit message, and updates the mempool.
// It returns the result of calling asura.Commit (the AppHash), and an error.
// The Mempool must be locked during commit and update because state is typically reset on Commit and old txs must be replayed
// against committed state before new txs are run in the mempool, lest they be invalid.
func (blockExec *BlockExecutor) Commit(block *types.Block) ([]byte, error) {
	blockExec.mempool.Lock()
	defer blockExec.mempool.Unlock()

	// while mempool is Locked, flush to ensure all async requests have completed
	// in the asura app before Commit.
	err := blockExec.mempool.FlushAppConn()
	if err != nil {
		blockExec.logger.Error("Client error during mempool.FlushAppConn", "err", err)
		return nil, err
	}

	// Commit block, get hash back
	res, err := blockExec.proxyApp.CommitSync()
	if err != nil {
		blockExec.logger.Error("Client error during proxyAppConn.CommitSync", "err", err)
		return nil, err
	}
	// ResponseCommit has no error code - just data

	blockExec.logger.Info("Committed state",
		"height", block.Height,
		"txs", block.NumTxs,
		"appHash", fmt.Sprintf("%X", res.Data))

	// Update mempool.
	if err := blockExec.mempool.Update(block.Height, block.Txs); err != nil {
		return nil, err
	}

	return res.Data, nil
}

//---------------------------------------------------------
// Helper functions for executing blocks and updating state

// Executes block's transactions on proxyAppConn.
// Returns a list of transaction results and updates to the validator set
func execBlockOnProxyApp(logger log.Logger, proxyAppConn proxy.AppConnConsensus, block *types.Block) (*AsuraResponses, error) {
	var validTxs, invalidTxs = 0, 0

	txIndex := 0
	AsuraResponses := NewAsuraResponses(block)

	// Execute transactions and get hash
	proxyCb := func(req *asura.Request, res *asura.Response) {
		switch r := res.Value.(type) {
		case *asura.Response_DeliverTx:
			// TODO: make use of res.Log
			// TODO: make use of this info
			// Blocks may include invalid txs.
			txRes := r.DeliverTx
			if txRes.Code == asura.CodeTypeOK {
				validTxs++
			} else {
				logger.Debug("Invalid tx", "code", txRes.Code, "log", txRes.Log)
				invalidTxs++
			}
			AsuraResponses.DeliverTx[txIndex] = txRes
			txIndex++
		}
	}
	proxyAppConn.SetResponseCallback(proxyCb)

	// determine which validators did not sign last block
	absentVals := make([]int32, 0)
	for valI, vote := range block.LastCommit.Precommits {
		if vote == nil {
			absentVals = append(absentVals, int32(valI))
		}
	}

	// TODO: determine which validators were byzantine
	byzantineVals := make([]asura.Evidence, len(block.Evidence.Evidence))
	for i, ev := range block.Evidence.Evidence {
		byzantineVals[i] = asura.Evidence{
			PubKey: ev.Address(), // XXX
			Height: ev.Height(),
		}
	}

	// Begin block
	_, err := proxyAppConn.BeginBlockSync(asura.RequestBeginBlock{
		Hash:                block.Hash(),
		Header:              types.TM2PB.Header(block.Header),
		AbsentValidators:    absentVals,
		ByzantineValidators: byzantineVals,
	})
	if err != nil {
		logger.Error("Error in proxyAppConn.BeginBlock", "err", err)
		return nil, err
	}

	// Run txs of block
	for _, tx := range block.Txs {
		proxyAppConn.DeliverTxAsync(tx)
		if err := proxyAppConn.Error(); err != nil {
			return nil, err
		}
	}

	// End block
	AsuraResponses.EndBlock, err = proxyAppConn.EndBlockSync(asura.RequestEndBlock{block.Height})
	if err != nil {
		logger.Error("Error in proxyAppConn.EndBlock", "err", err)
		return nil, err
	}

	logger.Info("Executed block", "height", block.Height, "validTxs", validTxs, "invalidTxs", invalidTxs)

	valUpdates := AsuraResponses.EndBlock.ValidatorUpdates
	if len(valUpdates) > 0 {
		logger.Info("Updates to validators", "updates", asura.ValidatorsString(valUpdates))
	}

	return AsuraResponses, nil
}

// If more or equal than 1/3 of total voting power changed in one block, then
// a light client could never prove the transition externally. See
// ./lite/doc.go for details on how a light client tracks validators.
func updateValidators(currentSet *types.ValidatorSet, updates []asura.Validator) error {
	for _, v := range updates {
		pubkey, err := crypto.PubKeyFromBytes(v.PubKey) // NOTE: expects go-amino encoded pubkey
		if err != nil {
			return err
		}

		address := pubkey.Address()
		power := int64(v.Power)
		// mind the overflow from int64
		if power < 0 {
			return fmt.Errorf("Power (%d) overflows int64", v.Power)
		}

		_, val := currentSet.GetByAddress(address)
		if val == nil {
			// add val
			added := currentSet.Add(types.NewValidator(pubkey, power))
			if !added {
				return fmt.Errorf("Failed to add new validator %X with voting power %d", address, power)
			}
		} else if v.Power == 0 {
			// remove val
			_, removed := currentSet.Remove(address)
			if !removed {
				return fmt.Errorf("Failed to remove validator %X", address)
			}
		} else {
			// update val
			val.VotingPower = power
			updated := currentSet.Update(val)
			if !updated {
				return fmt.Errorf("Failed to update validator %X with voting power %d", address, power)
			}
		}
	}
	return nil
}

// updateState returns a new State updated according to the header and responses.
func updateState(s State, blockID types.BlockID, header *types.Header,
	AsuraResponses *AsuraResponses) (State, error) {

	// copy the valset so we can apply changes from EndBlock
	// and update s.LastValidators and s.Validators
	prevValSet := s.Validators.Copy()
	nextValSet := prevValSet.Copy()

	// update the validator set with the latest AsuraResponses
	lastHeightValsChanged := s.LastHeightValidatorsChanged
	if len(AsuraResponses.EndBlock.ValidatorUpdates) > 0 {
		err := updateValidators(nextValSet, AsuraResponses.EndBlock.ValidatorUpdates)
		if err != nil {
			return s, fmt.Errorf("Error changing validator set: %v", err)
		}
		// change results from this height but only applies to the next height
		lastHeightValsChanged = header.Height + 1
	}

	// Update validator accums and set state variables
	nextValSet.IncrementAccum(1)

	// update the params with the latest AsuraResponses
	nextParams := s.ConsensusParams
	lastHeightParamsChanged := s.LastHeightConsensusParamsChanged
	if AsuraResponses.EndBlock.ConsensusParamUpdates != nil {
		// NOTE: must not mutate s.ConsensusParams
		nextParams = s.ConsensusParams.Update(AsuraResponses.EndBlock.ConsensusParamUpdates)
		err := nextParams.Validate()
		if err != nil {
			return s, fmt.Errorf("Error updating consensus params: %v", err)
		}
		// change results from this height but only applies to the next height
		lastHeightParamsChanged = header.Height + 1
	}

	// NOTE: the AppHash has not been populated.
	// It will be filled on state.Save.
	return State{
		ChainID:                          s.ChainID,
		LastBlockHeight:                  header.Height,
		LastBlockTotalTx:                 s.LastBlockTotalTx + header.NumTxs,
		LastBlockID:                      blockID,
		LastBlockTime:                    header.Time,
		Validators:                       nextValSet,
		LastValidators:                   s.Validators.Copy(),
		LastHeightValidatorsChanged:      lastHeightValsChanged,
		ConsensusParams:                  nextParams,
		LastHeightConsensusParamsChanged: lastHeightParamsChanged,
		LastResultsHash:                  AsuraResponses.ResultsHash(),
		AppHash:                          nil,
	}, nil
}

// Fire NewBlock, NewBlockHeader.
// Fire TxEvent for every tx.
// NOTE: if teragrid crashes before commit, some or all of these events may be published again.
func fireEvents(logger log.Logger, eventBus types.BlockEventPublisher, block *types.Block, AsuraResponses *AsuraResponses) {
	// NOTE: do we still need this buffer ?
	txEventBuffer := types.NewTxEventBuffer(eventBus, int(block.NumTxs))
	for i, tx := range block.Data.Txs {
		txEventBuffer.PublishEventTx(types.EventDataTx{types.TxResult{
			Height: block.Height,
			Index:  uint32(i),
			Tx:     tx,
			Result: *(AsuraResponses.DeliverTx[i]),
		}})
	}

	eventBus.PublishEventNewBlock(types.EventDataNewBlock{block})
	eventBus.PublishEventNewBlockHeader(types.EventDataNewBlockHeader{block.Header})
	err := txEventBuffer.Flush()
	if err != nil {
		logger.Error("Failed to flush event buffer", "err", err)
	}
}

//----------------------------------------------------------------------------------------------------
// Execute block without state. TODO: eliminate

// ExecCommitBlock executes and commits a block on the proxyApp without validating or mutating the state.
// It returns the application root hash (result of asura.Commit).
func ExecCommitBlock(appConnConsensus proxy.AppConnConsensus, block *types.Block, logger log.Logger) ([]byte, error) {
	_, err := execBlockOnProxyApp(logger, appConnConsensus, block)
	if err != nil {
		logger.Error("Error executing block on proxy app", "height", block.Height, "err", err)
		return nil, err
	}
	// Commit block, get hash back
	res, err := appConnConsensus.CommitSync()
	if err != nil {
		logger.Error("Client error during proxyAppConn.CommitSync", "err", res)
		return nil, err
	}
	// ResponseCommit has no error or log, just data
	return res.Data, nil
}