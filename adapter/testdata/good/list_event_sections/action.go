// Package list_event_sections is a fixture slice: Q5 keyset page with one
// INNER JOIN (sections to events for the event title). RULEBOOK.md quotes
// list_event_sections.en.
package list_event_sections

import (
	"context"
	"net/http"

	"example.com/fixtures/features/list_event_sections/db"
	"example.com/fixtures/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
)

const Route = "GET /events/{id}/sections"

var Roles = httpx.Public

type Input struct {
	EventID int64 `json:"event_id" path:"id"`
	After   int64 `json:"after" query:"after"`
	Limit   int64 `json:"limit" query:"limit"`
}

type Output struct {
	Sections  []domain.SectionOnEvent `json:"sections"`
	NextAfter int64                   `json:"next_after"`
}

var (
	F1 = failure.New("F1", http.StatusBadRequest, "page limit is out of range")
	F2 = failure.New("F2", http.StatusBadRequest, "page cursor must be greater than zero")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if !page.IsPageLimit(in.Limit) {
		return Output{}, F1
	}
	if in.After <= 0 {
		return Output{}, F2
	}

	rows, err := a.q.ListEventSections(ctx, db.ListEventSectionsParams{
		EventID: in.EventID,
		After:   in.After,
		Limit:   in.Limit,
	})
	if err != nil {
		return Output{}, err
	}

	items := make([]domain.SectionOnEvent, len(rows))
	for i, row := range rows {
		items[i] = domain.SectionOnEvent{
			ID:       row.ID,
			Name:     row.Name,
			Capacity: row.Capacity,
			Title:    row.Title,
		}
	}

	next := page.NextAfter(rows, "id", in.Limit)

	out := Output{Sections: items, NextAfter: next}
	assert.Post(out.Sections != nil, "an empty page is [], not null")
	return out, nil
}
