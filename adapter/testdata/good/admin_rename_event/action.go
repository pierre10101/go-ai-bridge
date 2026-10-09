// Package admin_rename_event is a fixture slice: it proves how bridge-en
// renders the A4 bypass (an action whose Roles lists only roles that
// cmd/server marks with httpx.AppRoles(...).BypassOwnership(...) may write
// an owned table without limiting the write to the signed-in user's rows,
// and the English says so). scripts/smoke-app.sh runs its checks.
package admin_rename_event

import (
	"context"
	"net/http"

	"example.com/fixtures/features/admin_rename_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /admin/events/title"

// Roles: only signed-in admins (A1), a role that bypasses ownership (A4).
var Roles = httpx.Roles("admin")

type Input struct {
	EventID int64  `json:"event_id"`
	Title   string `json:"title"`
}

type Output struct {
	EventID int64  `json:"event_id"`
	Title   string `json:"title"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "title is required")
	F2 = failure.New("F2", http.StatusNotFound, "event does not exist")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Title == "" {
		return Output{}, F1
	}

	renamed, err := a.q.RenameAnyEvent(ctx, db.RenameAnyEventParams{Title: in.Title, ID: in.EventID})
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
