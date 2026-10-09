// Package child_moves_parent is a deliberately refused fixture: a claim on a child table (A5) that proves the section's event is the signed-in user's, but changes the section's parent column (event_id) to an event the request names.
package child_moves_parent

import (
	"context"
	"net/http"

	"example.com/fixtures/features/child_moves_parent/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /sections/event"

var Roles = httpx.Roles("organizer")

type Input struct {
	SectionID  int64  `json:"section_id"`
	NewEventID int64  `json:"new_event_id"`
	Name       string `json:"name"`
	User       int64  `json:"user" server:"user"`
}

type Output struct {
	SectionID int64 `json:"section_id"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such section of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	moved, err := a.q.MoveSection(ctx, db.MoveSectionParams{NewEventID: in.NewEventID, Name: in.Name, ID: in.SectionID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if moved != 1 {
		return Output{}, F1
	}
	out := Output{SectionID: in.SectionID}
	assert.Post(out.SectionID == in.SectionID, "the answer names the moved section")
	return out, nil
}
