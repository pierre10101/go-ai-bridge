// Package delete_event is a fixture slice: it proves how bridge-en renders
// a Q10 delete on an owned table (A4: events is owned by organizer_id, so
// the delete has organizer_id = the signed-in user in its WHERE) and what
// schema.sql does to the rows that reference it: the event's sections are
// deleted with it (ON DELETE CASCADE), unless one of them still has seats
// (ON DELETE RESTRICT). scripts/smoke-app.sh runs its checks.
package delete_event

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /events"

// Roles: only signed-in organizers (A1), limited to their own events (A4).
var Roles = httpx.Roles("organizer")

type Input struct {
	EventID int64 `json:"event_id"`
	User    int64 `json:"user" server:"user"`
}

type Output struct {
	EventID int64 `json:"event_id"`
}

var F1 = failure.New("F1", http.StatusNotFound, "no such event of yours")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	deleted, err := a.q.DeleteOwnEvent(ctx, db.DeleteOwnEventParams{ID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if deleted != 1 {
		return Output{}, F1
	}

	out := Output{EventID: in.EventID}
	assert.Post(out.EventID == in.EventID, "the answer names the deleted event")
	return out, nil
}
