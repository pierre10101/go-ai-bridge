// Package owner_from_body is a deliberately refused fixture: writes to an owned table (A4) scoped by the owner column, but with a request field (organizer_id, which the caller sends) instead of the signed-in user (server:"user").
package owner_from_body

import (
	"context"
	"net/http"

	"example.com/fixtures/features/owner_from_body/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /events/copy"

var Roles = httpx.Roles("organizer")

type Input struct {
	EventID     int64  `json:"event_id"`
	OrganizerID int64  `json:"organizer_id"`
	Title       string `json:"title"`
	StartsAt    int64  `json:"starts_at"`
	User        int64  `json:"user" server:"user"`
}

type Output struct {
	EventID int64 `json:"event_id"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such event of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	renamed, err := a.q.RenameEvent(ctx, db.RenameEventParams{Title: in.Title, ID: in.EventID, OrganizerID: in.OrganizerID})
	if err != nil {
		return Output{}, err
	}
	if renamed != 1 {
		return Output{}, F1
	}
	row, err := a.q.CopyEvent(ctx, db.CopyEventParams{OrganizerID: in.OrganizerID, Title: in.Title, StartsAt: in.StartsAt})
	if err != nil {
		return Output{}, err
	}
	out := Output{EventID: row.ID}
	assert.Post(out.EventID != in.EventID, "the copy is a new event")
	return out, nil
}
