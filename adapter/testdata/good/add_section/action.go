// Package add_section is a fixture slice: it proves how bridge-en renders
// A5 for an insert (sections inherits its owner from events: a section is
// added only to an event whose organizer_id is the signed-in user, proved
// in the statement that writes, Q8, and checked like a claim, S10).
// RULEBOOK.md quotes add_section.en; scripts/smoke-app.sh builds it in a
// new app and runs its checks.
package add_section

import (
	"context"
	"net/http"

	"example.com/fixtures/features/add_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /sections"

// Roles: only signed-in organizers (A1); "organizer" does not bypass
// ownership, so a section is added only to the organizer's own events (A5).
var Roles = httpx.Roles("organizer")

type Input struct {
	EventID  int64  `json:"event_id"`
	Name     string `json:"name"`
	Capacity int64  `json:"capacity"`
	User     int64  `json:"user" server:"user"`
}

type Output struct {
	EventID int64  `json:"event_id"`
	Name    string `json:"name"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "name is required")
	F2 = failure.New("F2", http.StatusUnprocessableEntity, "capacity must be at least 1")
	F3 = failure.New("F3", http.StatusNotFound, "no such event of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Name == "" {
		return Output{}, F1
	}
	if in.Capacity < 1 {
		return Output{}, F2
	}

	added, err := a.q.AddSection(ctx, db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if added != 1 {
		return Output{}, F3
	}

	out := Output{EventID: in.EventID, Name: in.Name}
	assert.Post(out.EventID == in.EventID, "the answer names the event the section was added to")
	return out, nil
}
