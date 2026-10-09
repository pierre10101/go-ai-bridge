// Package read_clock_position is a deliberate rule break (T1, Q1, Q5, Q7):
// the current time in a read where it may not be compared. Each query in
// queries/reads.sql is refused before the action is read.
package read_clock_position

import (
	"context"
	"net/http"

	"example.com/fixtures/features/read_clock_position/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /tickets/ended"

type Input struct {
	TicketIDs []int64 `json:"ticket_ids" list:"1..20"`
	Session   string  `json:"session" server:"session"`
	Now       int64   `json:"now" clock:"now"`
}

type Output struct {
	Ended int64 `json:"ended"`
}

var F1 = failure.New("F1", http.StatusConflict, "a hold has ended")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	ended, err := a.q.CountAfterList(ctx, db.CountAfterListParams{Session: in.Session, Ids: in.TicketIDs, Now: in.Now})
	if err != nil {
		return Output{}, err
	}
	if ended != 0 {
		return Output{}, F1
	}
	out := Output{Ended: ended}
	assert.Post(out.Ended == 0, "no hold has ended")
	return out, nil
}
