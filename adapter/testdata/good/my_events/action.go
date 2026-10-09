// Package my_events is a fixture slice: it proves how bridge-en renders
// reads limited to the signed-in user's own rows, of an owned table (A4:
// organizer_id = in.User) and of a child table (A5: a Q9 subquery proves
// the section's event is the signed-in user's). scripts/smoke-app.sh runs
// its checks.
package my_events

import (
	"context"

	"example.com/fixtures/features/my_events/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "GET /me/events"

// Roles: only signed-in organizers (A1): "my events" means nothing to a
// visitor who is not signed in.
var Roles = httpx.Roles("organizer")

type Input struct {
	User int64 `json:"user" server:"user"`
}

type Output struct {
	Events   int64 `json:"events"`
	Sections int64 `json:"sections"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	eventCount, err := a.q.CountMyEvents(ctx, in.User)
	if err != nil {
		return Output{}, err
	}
	sectionCount, err := a.q.CountMySections(ctx, in.User)
	if err != nil {
		return Output{}, err
	}

	out := Output{Events: eventCount, Sections: sectionCount}
	assert.Post(out.Sections >= 0, "the number of sections is never negative")
	return out, nil
}
