// Package release_example is a fixture slice: it proves how bridge-en renders
// T2 (the session is set by the server from the session cookie) and what the
// English says about a claim's write when a later step stops the action: a
// guard that stops only when the claim changed nothing says nothing was
// written; a read or a guard before the count is known says any change is
// rolled back; a step after "exactly one row changed" says the write is
// rolled back. RULEBOOK.md quotes release_example.en; scripts/smoke-app.sh
// builds it in a new app and runs its checks.
package release_example

import (
	"context"
	"net/http"

	"example.com/fixtures/features/release_example/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /holds/release"

type Input struct {
	SeatID  int64 `json:"seat_id"`
	Session int64 `json:"session" server:"session"`
}

type Output struct {
	SeatID int64 `json:"seat_id"`
}

var (
	F1 = failure.New("F1", http.StatusUnauthorized, "session is required")
	F2 = failure.New("F2", http.StatusNotFound, "seat does not exist")
	F3 = failure.New("F3", http.StatusConflict, "seat is not held by this session")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Session <= 0 {
		return Output{}, F1
	}

	released, err := a.q.ReleaseSeat(ctx, db.ReleaseSeatParams{ID: in.SeatID, Session: in.Session})
	if err != nil {
		return Output{}, err
	}

	seats, err := a.q.CountSeats(ctx, in.SeatID)
	if err != nil {
		return Output{}, err
	}
	if released == 0 && seats == 0 {
		return Output{}, F2
	}

	seat, err := a.q.SeatHolder(ctx, in.SeatID)
	if err != nil {
		return Output{}, err
	}
	if released != 1 {
		return Output{}, F3
	}

	out := Output{SeatID: in.SeatID}
	assert.Post(seat.HeldBy == 0 && seat.HeldAt == 0, "nobody holds the seat any more")
	return out, nil
}
