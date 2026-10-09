// Package event_summary is a fixture slice: it proves how bridge-en renders
// a Public action (A1: anyone may call it, signed in or not) whose signed-in
// user and role (T3, server:"user" and server:"role") are 0 and the empty
// text when nobody is signed in, and the ownership sentences of reads (A4,
// A5): a read limited to the caller's rows and one that is not.
// scripts/smoke-app.sh runs its checks.
package event_summary

import (
	"context"

	"example.com/fixtures/features/event_summary/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "GET /events/{id}/summary"

// Roles: anyone may call it, signed in or not (A1).
var Roles = httpx.Public

type Input struct {
	EventID int64  `json:"event_id" path:"id"`
	User    int64  `json:"user" server:"user"`
	Role    string `json:"role" server:"role"`
}

type Output struct {
	EventID    int64  `json:"event_id"`
	Sections   int64  `json:"sections"`
	Yours      int64  `json:"yours"`
	ViewerRole string `json:"viewer_role"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	sections, err := a.q.CountSections(ctx, in.EventID)
	if err != nil {
		return Output{}, err
	}
	yours, err := a.q.CountOwnEvent(ctx, db.CountOwnEventParams{ID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}

	out := Output{EventID: in.EventID, Sections: sections, Yours: yours, ViewerRole: in.Role}
	assert.Post(out.ViewerRole == in.Role, "the answer carries the signed-in user's role")
	return out, nil
}
