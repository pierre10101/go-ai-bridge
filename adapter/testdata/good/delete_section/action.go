// Package delete_section is a fixture slice: it proves how bridge-en
// renders a Q10 delete on a child table (A5: sections inherits its owner
// from events, so the delete proves, with a Q9 subquery in its WHERE, that
// the section's event is the signed-in user's) and what schema.sql does to
// the section's seats (ON DELETE RESTRICT). scripts/smoke-app.sh runs its
// checks.
package delete_section

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /sections"

// Roles: only signed-in organizers (A1), limited to sections of their own
// events (A5).
var Roles = httpx.Roles("organizer")

type Input struct {
	SectionID int64 `json:"section_id"`
	User      int64 `json:"user" server:"user"`
}

type Output struct {
	SectionID int64 `json:"section_id"`
}

var F1 = failure.New("F1", http.StatusNotFound, "no such section of yours")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	deleted, err := a.q.DeleteOwnSection(ctx, db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if deleted != 1 {
		return Output{}, F1
	}

	out := Output{SectionID: in.SectionID}
	assert.Post(out.SectionID == in.SectionID, "the answer names the deleted section")
	return out, nil
}
