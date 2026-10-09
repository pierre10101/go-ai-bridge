// Package admin_delete_section is a fixture slice: it proves how bridge-en
// renders the ownership bypass on a Q10 delete (A5: an action whose Roles
// lists only roles that cmd/server marks with BypassOwnership may delete
// any section, and the English says so). scripts/smoke-app.sh runs its
// checks.
package admin_delete_section

import (
	"context"
	"net/http"

	"example.com/fixtures/features/admin_delete_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /admin/sections"

// Roles: only signed-in admins (A1), a role that bypasses ownership (A4, A5).
var Roles = httpx.Roles("admin")

type Input struct {
	SectionID int64 `json:"section_id"`
}

type Output struct {
	SectionID int64 `json:"section_id"`
}

var F1 = failure.New("F1", http.StatusNotFound, "no such section")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	deleted, err := a.q.DeleteAnySection(ctx, in.SectionID)
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
