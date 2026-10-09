// Package child_unscoped_insert is a deliberately refused fixture: writes to a child table (A5: sections inherits its owner from events) from an action organizers may call, with nothing that proves the event is the signed-in user's: an INSERT ... VALUES with the request's event id, and an UPDATE by id alone.
package child_unscoped_insert

import (
	"context"
	"net/http"

	"example.com/fixtures/features/child_unscoped_insert/db"
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
	User     int64  `json:"user" server:"user"`
}

type Output struct {
	SectionID int64 `json:"section_id"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such section")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	row, err := a.q.AddSection(ctx, db.AddSectionParams{EventID: in.EventID, Name: in.Name, Capacity: in.Capacity})
	if err != nil {
		return Output{}, err
	}
	renamed, err := a.q.RenameSection(ctx, db.RenameSectionParams{Name: in.Name, ID: row.ID})
	if err != nil {
		return Output{}, err
	}
	if renamed != 1 {
		return Output{}, F1
	}
	out := Output{SectionID: row.ID}
	assert.Post(out.SectionID == row.ID, "the answer names the new section")
	return out, nil
}
