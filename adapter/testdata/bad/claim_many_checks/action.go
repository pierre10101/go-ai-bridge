// Package claim_many_checks is a deliberate rule break (S11): a claim over
// IN (sqlc.slice(ids)) checked as if it changed one row, against the length
// of another list, only inside a compound condition, and len used outside
// the check. bridge-en refuses each.
package claim_many_checks

import (
	"context"
	"net/http"

	"example.com/fixtures/features/claim_many_checks/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /tickets/confirm"

type Input struct {
	TicketIDs []int64  `json:"ticket_ids" list:"1..20"`
	Codes     []string `json:"codes" list:"1..5"`
	Session   string   `json:"session" server:"session"`
	Now       int64    `json:"now" clock:"now"`
}

type Output struct {
	Confirmed int64 `json:"confirmed"`
}

var (
	F1 = failure.New("F1", http.StatusConflict, "a ticket is not held by this session")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	confirmed, err := a.q.ConfirmTickets(ctx, db.ConfirmTicketsParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs})
	if err != nil {
		return Output{}, err
	}
	if confirmed != 1 {
		return Output{}, F1
	}
	if confirmed != int64(len(in.Codes)) {
		return Output{}, F1
	}
	if confirmed == 0 || confirmed != int64(len(in.TicketIDs)) {
		return Output{}, F1
	}
	wanted := int64(len(in.TicketIDs))

	out := Output{Confirmed: wanted}
	assert.Post(out.Confirmed == confirmed, "every ticket is confirmed")
	return out, nil
}
