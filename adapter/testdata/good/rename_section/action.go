// Package rename_section is a fixture slice: it proves how bridge-en
// renders A5 for an update (sections inherits its owner from events: a
// claim on sections proves, with a Q9 subquery in its WHERE, that the
// section's event is the signed-in user's). scripts/smoke-app.sh runs its
// checks.
package rename_section

import (
	"context"
	"net/http"

	"example.com/fixtures/features/rename_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /sections/name"

// Roles: only signed-in organizers (A1), limited to sections of their own
// events (A5).
var Roles = httpx.Roles("organizer")

type Input struct {
	SectionID int64  `json:"section_id"`
	Name      string `json:"name"`
	User      int64  `json:"user" server:"user"`
}

type Output struct {
	SectionID int64  `json:"section_id"`
	Name      string `json:"name"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "name is required")
	F2 = failure.New("F2", http.StatusNotFound, "no such section of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Name == "" {
		return Output{}, F1
	}

	renamed, err := a.q.RenameOwnSection(ctx, db.RenameOwnSectionParams{Name: in.Name, ID: in.SectionID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if renamed != 1 {
		return Output{}, F2
	}

	out := Output{SectionID: in.SectionID, Name: in.Name}
	assert.Post(out.SectionID == in.SectionID, "the answer names the renamed section")
	return out, nil
}
