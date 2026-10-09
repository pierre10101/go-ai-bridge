// Package clock_inside is a deliberate rule break (T1): the action reads the
// clock itself, so no check can choose the time and the English cannot say
// which time a rule uses. bridge-en refuses it.
package clock_inside

import (
	"context"
	"net/http"
	"time"

	"example.com/fixtures/features/clock_inside/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /holds"

type Input struct {
	SeatID int64  `json:"seat_id"`
	Now    string `json:"now" clock:"now"`
}

type Output struct {
	HeldAt int64 `json:"held_at"`
}

var F1 = failure.New("F1", http.StatusConflict, "seat is already held")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	now := time.Now().Unix()
	if in.SeatID <= 0 {
		return Output{}, F1
	}
	out := Output{HeldAt: now}
	assert.Post(out.HeldAt > 0, "held at a real time")
	return out, nil
}

// Roles: anyone may call it (A1); this fixture is about another rule.
var Roles = httpx.Public
