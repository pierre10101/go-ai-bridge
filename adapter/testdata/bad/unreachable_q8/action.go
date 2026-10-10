// Package unreachable_q8 is a deliberate rule break (S10): a Q6 claim on
// events proves the parent row, so a later Q8 insert from events can never
// get added != 1. bridge-en refuses the unreachable guard.
package unreachable_q8

import (
	"context"
	"net/http"

	"example.com/fixtures/features/unreachable_q8/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /sections"

var Roles = httpx.Roles("organizer")

type Input struct {
	EventID  int64  `json:"event_id"`
	Name     string `json:"name"`
	Capacity int64  `json:"capacity"`
	Title    string `json:"title"`
	User     int64  `json:"user" server:"user"`
}

type Output struct {
	EventID int64  `json:"event_id"`
	Name    string `json:"name"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such event of yours")
	F2 = failure.New("F2", http.StatusNotFound, "no such event of yours for the section")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	touched, err := a.q.TouchEvent(ctx, db.TouchEventParams{Title: in.Title, ID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if touched != 1 {
		return Output{}, F1
	}

	added, err := a.q.AddSection(ctx, db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if added != 1 {
		return Output{}, F2
	}

	out := Output{EventID: in.EventID, Name: in.Name}
	assert.Post(out.EventID == in.EventID, "the answer names the event")
	return out, nil
}
