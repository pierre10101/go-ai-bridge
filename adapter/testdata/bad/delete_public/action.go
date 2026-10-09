// Package delete_public is a deliberately refused fixture: a Public action that deletes from an owned table (A4) and from a child table (A5). Signed out, its user is 0, which owns nothing, so a Public action never deletes either, even with the owner condition and the proof in place.
package delete_public

import (
	"context"
	"net/http"

	"example.com/fixtures/features/delete_public/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "DELETE /events"

var Roles = httpx.Public

type Input struct {
	EventID   int64 `json:"event_id"`
	SectionID int64 `json:"section_id"`
	User      int64 `json:"user" server:"user"`
}

type Output struct {
	EventID int64 `json:"event_id"`
}

var (
	F1 = failure.New("F1", http.StatusNotFound, "no such section of yours")
	F2 = failure.New("F2", http.StatusNotFound, "no such event of yours")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	sections, err := a.q.DeleteOwnSection(ctx, db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if sections != 1 {
		return Output{}, F1
	}
	events, err := a.q.DeleteOwnEvent(ctx, db.DeleteOwnEventParams{ID: in.EventID, OrganizerID: in.User})
	if err != nil {
		return Output{}, err
	}
	if events != 1 {
		return Output{}, F2
	}
	out := Output{EventID: in.EventID}
	assert.Post(out.EventID == in.EventID, "the answer names the deleted row")
	return out, nil
}
