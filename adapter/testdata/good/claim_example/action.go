// Package claim_example is a fixture slice: it proves how bridge-en renders
// T1 (the current time is passed in), T2 (the caller is the server-set
// session, never an id from the request), Q6 (a claim UPDATE that writes an
// expiry) and S10 (exactly one row changed). RULEBOOK.md quotes claim_example.en; scripts/smoke-app.sh
// builds it in a new app and runs its checks.
package claim_example

import (
	"context"
	"net/http"

	"example.com/fixtures/features/claim_example/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /holds"

type Input struct {
	SeatID  int64 `json:"seat_id"`
	Session int64 `json:"session" server:"session"`
	Now     int64 `json:"now" clock:"now"`
}

type Output struct {
	SeatID int64 `json:"seat_id"`
	Now    int64 `json:"now"`
}

var (
	F1 = failure.New("F1", http.StatusConflict, "seat is already held")
	F2 = failure.New("F2", http.StatusUnauthorized, "session is required")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Session <= 0 {
		return Output{}, F2
	}

	claimed, err := a.q.ClaimSeat(ctx, db.ClaimSeatParams{
		Session: in.Session,
		Now:     in.Now,
		ID:      in.SeatID,
	})
	if err != nil {
		return Output{}, err
	}
	if claimed != 1 {
		return Output{}, F1
	}

	out := Output{SeatID: in.SeatID, Now: in.Now}
	assert.Post(out.Now == in.Now, "the answer carries the server's clock")
	return out, nil
}
