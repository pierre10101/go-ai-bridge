// Package delete_owned_unscoped is a deliberately refused fixture: a Q10 delete from an owned table (A4: events is owned by organizer_id) from an action organizers may call, by id alone: organizer B could delete organizer A's event.
package delete_owned_unscoped

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_owned_unscoped/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /events"

var Roles = httpx.Roles("organizer")

type Input struct {
	EventID int64 `json:"event_id"`
	User    int64 `json:"user" server:"user"`
}

type Output struct {
	EventID int64 `json:"event_id"`
}

var F1 = failure.New("F1", http.StatusNotFound, "no such event of yours")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	deleted, err := a.q.DeleteEvent(ctx, in.EventID)
	if err != nil {
		return Output{}, err
	}
	if deleted != 1 {
		return Output{}, F1
	}
	out := Output{EventID: in.EventID}
	assert.Post(out.EventID == in.EventID, "the answer names the deleted row")
	return out, nil
}
