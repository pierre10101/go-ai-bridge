// Package my_events is a fixture slice: it proves how bridge-en renders a
// Public action (A1: anyone may call it, signed in or not) whose signed-in
// user and role (T3, server:"user" and server:"role") are 0 and the empty
// text when nobody is signed in. scripts/smoke-app.sh runs its checks.
package my_events

import (
	"context"

	"example.com/fixtures/features/my_events/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "GET /me/events"

// Roles: anyone may call it, signed in or not (A1).
var Roles = httpx.Public

type Input struct {
	User int64  `json:"user" server:"user"`
	Role string `json:"role" server:"role"`
}

type Output struct {
	Events int64  `json:"events"`
	Role   string `json:"role"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	events, err := a.q.CountMyEvents(ctx, in.User)
	if err != nil {
		return Output{}, err
	}

	out := Output{Events: events, Role: in.Role}
	assert.Post(out.Role == in.Role, "the answer carries the signed-in user's role")
	return out, nil
}
