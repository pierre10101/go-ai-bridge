// Package identity_from_body is a deliberately refused fixture: the signed-in user and role taken from the request body (T3) instead of server:"user" and server:"role".
package identity_from_body

import (
	"context"
	"net/http"

	"example.com/fixtures/features/identity_from_body/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /events"

var Roles = httpx.Roles("organizer", "admin")

type Input struct {
	Title  string `json:"title"`
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
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
