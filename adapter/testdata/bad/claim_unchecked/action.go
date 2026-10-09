// Package claim_unchecked is a deliberate rule break (S10): the claim result is
// only checked for 0, so a claim that changed several rows would succeed.
// bridge-en refuses it, and refuses a comparison like claimed > 0 outright.
package claim_unchecked

import (
	"context"
	"net/http"

	"example.com/fixtures/features/claim_unchecked/db"
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
	if claimed == 0 {
		return Output{}, F1
	}

	if claimed > 1 {
		return Output{}, F1
	}

	out := Output{SeatID: in.SeatID, HeldBy: in.PersonID, HeldAt: in.Now}
	assert.Post(out.HeldBy == in.PersonID, "the hold belongs to the requester")
	return out, nil
}
