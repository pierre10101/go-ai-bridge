// Package owner_unscoped is a deliberately refused fixture: an UPDATE of an owned table (A4, schema.sql "-- owner: organizer_id") that does not limit its rows to the signed-in user's, from an action an organizer may call ("organizer" does not bypass ownership; "admin" does, but not every listed role does).
package owner_unscoped

import (
	"context"
	"net/http"

	"example.com/fixtures/features/owner_unscoped/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "PATCH /events/title"

var Roles = httpx.Roles("organizer", "admin")

type Input struct {
	EventID int64  `json:"event_id"`
	Title   string `json:"title"`
	User    int64  `json:"user" server:"user"`
}

type Output struct {
	EventID int64 `json:"event_id"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such event")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	renamed, err := a.q.RenameEvent(ctx, db.RenameEventParams{Title: in.Title, ID: in.EventID})
	if err != nil {
		return Output{}, err
	}
	if renamed != 1 {
		return Output{}, F1
	}
	out := Output{EventID: in.EventID}
	assert.Post(out.EventID == in.EventID, "the answer names the event")
	return out, nil
}
