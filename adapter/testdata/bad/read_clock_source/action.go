// Package read_clock_source is a deliberate rule break (T1, Q1, Q2): reads
// that compare a column with something other than the action's server-set
// clock input. The English of a comparison in a read says "the current time"
// in the Q6 boundary words, so bridge-en refuses any other source: a request
// field, a parameter named now bound to another value, a literal.
package read_clock_source

import (
	"context"
	"net/http"

	"example.com/fixtures/features/read_clock_source/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /tickets/held"

type Input struct {
	TicketIDs []int64 `json:"ticket_ids" list:"1..20"`
	Cutoff    int64   `json:"cutoff"`
	Session   string  `json:"session" server:"session"`
	Now       int64   `json:"now" clock:"now"`
}

type Output struct {
	Held int64 `json:"held"`
}

var (
	F1 = failure.New("F1", http.StatusConflict, "a hold has ended")
	F2 = failure.New("F2", http.StatusNotFound, "no live hold")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	// The clock compared with a parameter the caller sends.
	held, err := a.q.CountHeldUntil(ctx, db.CountHeldUntilParams{Session: in.Session, Cutoff: in.Cutoff, Ids: in.TicketIDs})
	if err != nil {
		return Output{}, err
	}
	if held != 0 {
		return Output{}, F1
	}
	// A parameter named now, bound to something other than the clock input.
	hold, err := a.q.FindLiveHold(ctx, db.FindLiveHoldParams{Session: in.Session, Now: in.Cutoff})
	if err != nil {
		return Output{}, err
	}
	if hold.ExpiresAt == 0 {
		return Output{}, F2
	}
	out := Output{Held: held}
	assert.Post(out.Held == 0, "no hold has ended")
	return out, nil
}

// Roles: anyone may call it (A1); this fixture is about another rule.
var Roles = httpx.Public
