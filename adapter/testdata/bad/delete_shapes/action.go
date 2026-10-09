// Package delete_shapes is a deliberately refused fixture: Q10 deletes outside the allowed shape, from an admin-only action (the ownership bypass, so only the shape is at stake): no WHERE, a WHERE that does not name the rows by the key, a range, an OR, RETURNING, and a delete from a table that a foreign key references without ON DELETE CASCADE or RESTRICT.
package delete_shapes

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_shapes/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /admin/sections"

var Roles = httpx.Roles("admin")

type Input struct {
	SectionID int64  `json:"section_id"`
	Name      string `json:"name"`
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
	deleted, err := a.q.DeleteByName(ctx, in.Name)
	if err != nil {
		return Output{}, err
	}
	if deleted != 1 {
		return Output{}, F1
	}
	out := Output{SectionID: in.SectionID}
	assert.Post(out.SectionID == in.SectionID, "the answer names the deleted row")
	return out, nil
}
