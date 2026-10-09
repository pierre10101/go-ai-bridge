// Package roles_missing is a deliberately refused fixture: an action that does not declare who may call it (A1).
package roles_missing

import (
	"context"
	"net/http"

	"example.com/fixtures/features/roles_missing/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /events"

type Input struct {
	Title string `json:"title"`
	User  int64  `json:"user" server:"user"`
}

type Output struct {
	Title string `json:"title"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "title is required")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Title == "" {
		return Output{}, F1
	}
	out := Output{Title: in.Title}
	assert.Post(out.Title == in.Title, "the answer carries the title")
	return out, nil
}
