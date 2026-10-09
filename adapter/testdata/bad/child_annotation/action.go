// Package child_annotation is a deliberately refused fixture: malformed A5 inherited owner annotations in schema.sql (an unknown parent column, an unknown parent table, a parent without an owner, a parent column other than the parent's own, a type that is not the parent key's, a parent without a single-column PRIMARY KEY, a chain that comes back to itself, a malformed arrow), and a Q9 subquery on a table whose annotation is refused.
package child_annotation

import (
	"context"

	"example.com/childbad/features/child_annotation/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "GET /me/sections"

var Roles = httpx.Roles("organizer")

type Input struct {
	User int64 `json:"user" server:"user"`
}

type Output struct {
	Sections int64 `json:"sections"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	sections, err := a.q.CountSections(ctx, in.User)
	if err != nil {
		return Output{}, err
	}
	out := Output{Sections: sections}
	assert.Post(out.Sections >= 0, "a count is never negative")
	return out, nil
}
