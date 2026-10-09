// Package claim_compound_check is a deliberate rule break (S10): the only
// guard on the claim's count is claimed == 0 && claimed != 1, which is
// claimed == 0. A claim that changed 2 or more rows (an id that is not
// unique, a WHERE that matches too much) would pass it, while the English
// would still say "not exactly one seat was changed". bridge-en refuses it:
// the check is a guard whose entire condition is claimed != 1.
package claim_compound_check

import (
	"context"
	"net/http"

	"example.com/fixtures/features/claim_compound_check/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
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
	if claimed == 0 && claimed != 1 {
		return Output{}, F1
	}

	out := Output{SeatID: in.SeatID, Now: in.Now}
	assert.Post(out.Now == in.Now, "the answer carries the server's clock")
	return out, nil
}

// Roles: anyone may call it (A1); this fixture is about another rule.
var Roles = httpx.Public
