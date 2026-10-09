// Package owner_annotation is a deliberately refused fixture: malformed A4 owner annotations in schema.sql (an unknown column, a second annotation, a REAL column, two columns, one attached to no CREATE TABLE), and a signed-in user field whose type (int64) is not that of the owner column it is compared with (notes.author is TEXT).
package owner_annotation

import (
	"context"

	"example.com/ownerbad/features/owner_annotation/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "GET /me/notes"

var Roles = httpx.Public

type Input struct {
	User int64 `json:"user" server:"user"`
}

type Output struct {
	Notes int64 `json:"notes"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	notes, err := a.q.CountMyNotes(ctx, in.User)
	if err != nil {
		return Output{}, err
	}
	out := Output{Notes: notes}
	assert.Post(out.Notes >= 0, "a count is never negative")
	return out, nil
}
