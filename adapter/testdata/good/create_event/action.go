// Package create_event is a fixture slice: it proves how bridge-en renders
// A1 (an action that only some roles may call: httpx.Bind answers 401 or
// 403 before Handle runs) and T3 (the owner is the signed-in user from the
// server, server:"user", never an id from the request). RULEBOOK.md quotes
// create_event.en; scripts/smoke-app.sh builds it in a new app and runs its
// checks.
package create_event

import (
	"context"
	"net/http"

	"example.com/fixtures/features/create_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /events"

// Roles: only signed-in organizers and admins may call it (A1).
var Roles = httpx.Roles("organizer", "admin")

type Input struct {
	Title    string `json:"title"`
	StartsAt int64  `json:"starts_at"`
	User     int64  `json:"user" server:"user"`
	Role     string `json:"role" server:"role"`
	Now      int64  `json:"now" clock:"now"`
}

type Output struct {
	EventID     int64  `json:"event_id"`
	OrganizerID int64  `json:"organizer_id"`
	CreatedAs   string `json:"created_as"`
}

var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "title is required")
	F2 = failure.New("F2", http.StatusUnprocessableEntity, "the event must start later than now")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Title == "" {
		return Output{}, F1
	}
	if in.StartsAt <= in.Now {
		return Output{}, F2
	}

	row, err := a.q.InsertEvent(ctx, db.InsertEventParams{
		OrganizerID: in.User,
		CreatedAs:   in.Role,
		Title:       in.Title,
		StartsAt:    in.StartsAt,
	})
	if err != nil {
		return Output{}, err
	}

	out := Output{EventID: row.ID, OrganizerID: row.OrganizerID, CreatedAs: row.CreatedAs}
	assert.Post(out.OrganizerID == in.User, "the event belongs to the signed-in user")
	return out, nil
}
