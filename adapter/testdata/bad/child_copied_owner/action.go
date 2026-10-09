// Package child_copied_owner is a deliberately refused fixture: writes to a child table (A5) that "prove" ownership with the child's own copy of organizer_id (set to, or compared with, the signed-in user) instead of the parent event's, and a Q8 insert that copies the event's organizer_id onto the section.
package child_copied_owner

import (
	"context"
	"net/http"

	"example.com/copiedowner/features/child_copied_owner/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /sections"

var Roles = httpx.Roles("organizer")

type Input struct {
	EventID int64  `json:"event_id"`
	Name    string `json:"name"`
	User    int64  `json:"user" server:"user"`
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
	row, err := a.q.AddSection(ctx, db.AddSectionParams{EventID: in.EventID, OrganizerID: in.User, Name: in.Name})
	if err != nil {
		return Output{}, err
	}
	renamed, err := a.q.RenameSection(ctx, db.RenameSectionParams{Name: in.Name, ID: row.ID, OrganizerID: in.User})
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
