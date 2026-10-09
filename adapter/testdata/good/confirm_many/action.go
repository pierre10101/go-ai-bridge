// Package confirm_many is a fixture slice: it proves how bridge-en renders
// D10 (a list input with bounds and no duplicates), Q7 (IN (sqlc.slice(...))
// in a claim and in reads), a Q1 read that compares with the current time
// (T1) before its IN list, and S11 (a multi-row claim must change exactly one
// row per entry of the list, or everything is rolled back). RULEBOOK.md
// quotes confirm_many.en; scripts/smoke-app.sh builds it in a new app and
// runs its checks against SQLite.
package confirm_many

import (
	"context"
	"net/http"

	"example.com/fixtures/features/confirm_many/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /tickets/confirm"

// Roles: anyone may call it, signed in or not; the caller is the session
// from the cookie (A1).
var Roles = httpx.Public

type Input struct {
	TicketIDs []int64 `json:"ticket_ids" list:"1..20"`
	Session   string  `json:"session" server:"session"`
	Now       int64   `json:"now" clock:"now"`
}

type Output struct {
	Confirmed int64 `json:"confirmed"`
}

var (
	F1 = failure.New("F1", http.StatusUnauthorized, "session is required")
	F2 = failure.New("F2", http.StatusGone, "a hold has expired")
	F3 = failure.New("F3", http.StatusConflict, "a ticket is not held by this session")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Session == "" {
		return Output{}, F1
	}

	confirmed, err := a.q.ConfirmTickets(ctx, db.ConfirmTicketsParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs})
	if err != nil {
		return Output{}, err
	}

	held, err := a.q.CountStillHeld(ctx, db.CountStillHeldParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs})
	if err != nil {
		return Output{}, err
	}
	if held != 0 {
		return Output{}, F2
	}
	if confirmed != int64(len(in.TicketIDs)) {
		return Output{}, F3
	}

	sold, err := a.q.CountSold(ctx, db.CountSoldParams{Session: in.Session, Ids: in.TicketIDs})
	if err != nil {
		return Output{}, err
	}

	out := Output{Confirmed: confirmed}
	assert.Post(sold == confirmed, "every ticket changed is sold to this session")
	return out, nil
}
