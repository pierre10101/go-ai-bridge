// Package rename_event is a fixture slice: it proves how bridge-en renders
// A4 (an UPDATE of an owned table, from an action that a role without the
// ownership bypass may call, is limited to the signed-in user's own rows:
// organizer_id = in.User in its WHERE). RULEBOOK.md quotes rename_event.en;
// scripts/smoke-app.sh builds it in a new app and runs its checks.
package rename_event

import (
	"context"
	"net/http"

	"example.com/fixtures/features/rename_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /events/{id}/title"

// Roles: only signed-in organizers (A1); "organizer" does not bypass
// ownership, so the rename is limited to the organizer's own events (A4).
var Roles = httpx.Roles("organizer")

type Input struct {
	EventID int64  `json:"event_id" path:"id"`
	Title   string `json:"title"`
	User    int64  `json:"user" server:"user"`
}

type Output struct {
	EventID int64  `json:"event_id"`
	Title   string `json:"title"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "title is required")
	F2 = failure.New("F2", http.StatusNotFound, "no such event of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Title == "" {
		return Output{}, F1
	}

	renamed, err := a.q.RenameOwnEvent(ctx, db.RenameOwnEventParams{Title: in.Title, ID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if renamed != 1 {
		return Output{}, F2
	}

	out := Output{EventID: in.EventID, Title: in.Title}
	assert.Post(out.EventID == in.EventID, "the answer names the renamed event")
	return out, nil
}
