// Package claim_example is a fixture slice: it proves how bridge-en renders
// T1 (the current time is passed in), Q6 (a claim UPDATE) and S10 (exactly
// one row changed). RULEBOOK.md quotes claim_example.en; scripts/smoke-app.sh
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
	SeatID   int64 `json:"seat_id"`
	PersonID int64 `json:"person_id"`
	Now      int64 `json:"now" clock:"now"`
}

type Output struct {
	SeatID int64 `json:"seat_id"`
	HeldBy int64 `json:"held_by"`
	HeldAt int64 `json:"held_at"`
}

var (
	F1 = failure.New("F1", http.StatusConflict, "seat is already held")
	F2 = failure.New("F2", http.StatusUnprocessableEntity, "person is required")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.PersonID <= 0 {
		return Output{}, F2
	}

	claimed, err := a.q.ClaimSeat(ctx, db.ClaimSeatParams{
		HeldBy: in.PersonID,
		Now:    in.Now,
		ID:     in.SeatID,
	})
	if err != nil {
		return Output{}, err
	}
	if claimed != 1 {
		return Output{}, F1
	}

	out := Output{SeatID: in.SeatID, HeldBy: in.PersonID, HeldAt: in.Now}
	assert.Post(out.HeldBy == in.PersonID, "the hold belongs to the requester")
	return out, nil
}
