// Package admin_rename_section is a fixture slice: it proves how bridge-en
// renders the ownership bypass on a child table (A5: an action whose Roles
// lists only roles that cmd/server marks with BypassOwnership may write
// sections without proving the event is the signed-in user's, and the
// English says so). scripts/smoke-app.sh runs its checks.
package admin_rename_section

import (
	"context"
	"net/http"

	"example.com/fixtures/features/admin_rename_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /admin/sections/name"

// Roles: only signed-in admins (A1), a role that bypasses ownership (A4, A5).
var Roles = httpx.Roles("admin")

type Input struct {
	SectionID int64  `json:"section_id"`
	Name      string `json:"name"`
}

type Output struct {
	SectionID int64  `json:"section_id"`
	Name      string `json:"name"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "name is required")
	F2 = failure.New("F2", http.StatusNotFound, "no such section")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Name == "" {
		return Output{}, F1
	}

	renamed, err := a.q.RenameAnySection(ctx, db.RenameAnySectionParams{Name: in.Name, ID: in.SectionID})
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
