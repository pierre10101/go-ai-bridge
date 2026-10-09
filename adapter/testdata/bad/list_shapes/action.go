// Package list_shapes is a deliberate rule break (D10, Q7): list inputs
// without bounds, with bad bounds or element types, a list tag outside a
// list input, a list used as a value, and IN lists the grammar refuses.
package list_shapes

import (
	"context"
	"net/http"

	"example.com/fixtures/features/list_shapes/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /tickets/count"

type Input struct {
	NoTag   []int64 `json:"no_tag"`
	BadTag  []int64 `json:"bad_tag" list:"0..500"`
	Flags   []bool  `json:"flags" list:"1..3"`
	Scalar  int64   `json:"scalar" list:"1..3"`
	SeatIDs []int64 `json:"seat_ids" list:"1..20"`
}

type Output struct {
	Count int64 `json:"count" list:"1..3"`
}

var (
	F1 = failure.New("F1", http.StatusBadRequest, "seats are required")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.SeatIDs == nil {
		return Output{}, F1
	}
	n, err := a.q.CountTickets(ctx, in.Scalar)
	if err != nil {
		return Output{}, err
	}
	out := Output{Count: n}
	assert.Post(out.Count >= 0, "a count is never negative")
	return out, nil
}
