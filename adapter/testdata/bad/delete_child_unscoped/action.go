// Package delete_child_unscoped is a deliberately refused fixture: a Q10 delete from a child table (A5: sections inherits its owner from events) from an action organizers may call, by id alone, with nothing that proves the section's event is the signed-in user's.
package delete_child_unscoped

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_child_unscoped/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /sections"

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
	deleted, err := a.q.DeleteSection(ctx, in.SectionID)
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
