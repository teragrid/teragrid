package types

import (
	"fmt"

	amino "github.com/teragrid/dgrid/third_party/amino"
	asura "github.com/teragrid/dgrid/asura/types"
	tpubsub "github.com/teragrid/dgrid/pkg/pubsub"
	tquery "github.com/teragrid/dgrid/pkg/pubsub/query"
)

// Reserved event types (alphabetically sorted).
const (
	// Block level events for mass consumption by users.
	// These events are triggered from the state package,
	// after a block has been committed.
	// These are also used by the tx indexer for async indexing.
	// All of this data can be fetched through the rpc.
	EventNewBlock            = "NewBlock"
	EventNewBlockHeader      = "NewBlockHeader"
	EventTx                  = "Tx"
	EventValidatorSetUpdates = "ValidatorSetUpdates"

	// Internal consensus events.
	// These are used for testing the consensus state machine.
	// They can also be used to build real-time consensus visualizers.
	EventCompleteProposal = "CompleteProposal"
	EventLock             = "Lock"
	EventNewRound         = "NewRound"
	EventNewRoundStep     = "NewRoundStep"
	EventPolka            = "Polka"
	EventRelock           = "Relock"
	EventTimeoutPropose   = "TimeoutPropose"
	EventTimeoutWait      = "TimeoutWait"
	EventUnlock           = "Unlock"
	EventValidBlock       = "ValidBlock"
	EventVote             = "Vote"
)

///////////////////////////////////////////////////////////////////////////////
// ENCODING / DECODING
///////////////////////////////////////////////////////////////////////////////

// TMEventData implements events.EventData.
type TMEventData interface {
	// empty interface
}

func RegisterEventDatas(cdc *amino.Codec) {
	cdc.RegisterInterface((*TMEventData)(nil), nil)
	cdc.RegisterConcrete(EventDataNewBlock{}, "teragrid/event/NewBlock", nil)
	cdc.RegisterConcrete(EventDataNewBlockHeader{}, "teragrid/event/NewBlockHeader", nil)
	cdc.RegisterConcrete(EventDataTx{}, "teragrid/event/Tx", nil)
	cdc.RegisterConcrete(EventDataRoundState{}, "teragrid/event/RoundState", nil)
	cdc.RegisterConcrete(EventDataNewRound{}, "teragrid/event/NewRound", nil)
	cdc.RegisterConcrete(EventDataCompleteProposal{}, "teragrid/event/CompleteProposal", nil)
	cdc.RegisterConcrete(EventDataVote{}, "teragrid/event/Vote", nil)
	cdc.RegisterConcrete(EventDataValidatorSetUpdates{}, "teragrid/event/ValidatorSetUpdates", nil)
	cdc.RegisterConcrete(EventDataString(""), "teragrid/event/ProposalString", nil)
}

// Most event messages are basic types (a block, a transaction)
// but some (an input to a call tx or a receive) are more exotic

type EventDataNewBlock struct {
	Block *Block `json:"block"`

	ResultBeginBlock asura.ResponseBeginBlock `json:"result_begin_block"`
	ResultEndBlock   asura.ResponseEndBlock   `json:"result_end_block"`
}

// light weight event for benchmarking
type EventDataNewBlockHeader struct {
	Header Header `json:"header"`

	ResultBeginBlock asura.ResponseBeginBlock `json:"result_begin_block"`
	ResultEndBlock   asura.ResponseEndBlock   `json:"result_end_block"`
}

// All txs fire EventDataTx
type EventDataTx struct {
	TxResult
}

// NOTE: This goes into the replay WAL
type EventDataRoundState struct {
	Height int64  `json:"height"`
	Round  int    `json:"round"`
	Step   string `json:"step"`
}

type ValidatorInfo struct {
	Address Address `json:"address"`
	Index   int     `json:"index"`
}

type EventDataNewRound struct {
	Height int64  `json:"height"`
	Round  int    `json:"round"`
	Step   string `json:"step"`

	Proposer ValidatorInfo `json:"proposer"`
}

type EventDataCompleteProposal struct {
	Height int64  `json:"height"`
	Round  int    `json:"round"`
	Step   string `json:"step"`

	BlockID BlockID `json:"block_id"`
}

type EventDataVote struct {
	Vote *Vote
}

type EventDataString string

type EventDataValidatorSetUpdates struct {
	ValidatorUpdates []*Validator `json:"validator_updates"`
}

///////////////////////////////////////////////////////////////////////////////
// PUBSUB
///////////////////////////////////////////////////////////////////////////////

const (
	// EventTypeKey is a reserved key, used to specify event type in tags.
	EventTypeKey = "tm.event"
	// TxHashKey is a reserved key, used to specify transaction's hash.
	// see EventBus#PublishEventTx
	TxHashKey = "tx.hash"
	// TxHeightKey is a reserved key, used to specify transaction block's height.
	// see EventBus#PublishEventTx
	TxHeightKey = "tx.height"
)

var (
	EventQueryCompleteProposal    = QueryForEvent(EventCompleteProposal)
	EventQueryLock                = QueryForEvent(EventLock)
	EventQueryNewBlock            = QueryForEvent(EventNewBlock)
	EventQueryNewBlockHeader      = QueryForEvent(EventNewBlockHeader)
	EventQueryNewRound            = QueryForEvent(EventNewRound)
	EventQueryNewRoundStep        = QueryForEvent(EventNewRoundStep)
	EventQueryPolka               = QueryForEvent(EventPolka)
	EventQueryRelock              = QueryForEvent(EventRelock)
	EventQueryTimeoutPropose      = QueryForEvent(EventTimeoutPropose)
	EventQueryTimeoutWait         = QueryForEvent(EventTimeoutWait)
	EventQueryTx                  = QueryForEvent(EventTx)
	EventQueryUnlock              = QueryForEvent(EventUnlock)
	EventQueryValidatorSetUpdates = QueryForEvent(EventValidatorSetUpdates)
	EventQueryValidBlock          = QueryForEvent(EventValidBlock)
	EventQueryVote                = QueryForEvent(EventVote)
)

func EventQueryTxFor(tx Tx) tpubsub.Query {
	return tquery.MustParse(fmt.Sprintf("%s='%s' AND %s='%X'", EventTypeKey, EventTx, TxHashKey, tx.Hash()))
}

func QueryForEvent(eventType string) tpubsub.Query {
	return tquery.MustParse(fmt.Sprintf("%s='%s'", EventTypeKey, eventType))
}

// BlockEventPublisher publishes all block related events
type BlockEventPublisher interface {
	PublishEventNewBlock(block EventDataNewBlock) error
	PublishEventNewBlockHeader(header EventDataNewBlockHeader) error
	PublishEventTx(EventDataTx) error
	PublishEventValidatorSetUpdates(EventDataValidatorSetUpdates) error
}

type TxEventPublisher interface {
	PublishEventTx(EventDataTx) error
}
