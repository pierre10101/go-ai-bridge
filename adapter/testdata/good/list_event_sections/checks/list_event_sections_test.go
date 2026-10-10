package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/list_event_sections"
	"example.com/fixtures/features/list_event_sections/db"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func newAction(t *testing.T) (*list_event_sections.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`
		INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'Night Show', 1);
		INSERT INTO sections (id, event_id, name, capacity) VALUES (3, 1, 'Balcony', 40), (2, 1, 'Pit', 100), (1, 1, 'Gallery', 20);
	`); err != nil {
		t.Fatal(err)
	}
	return list_event_sections.New(db.New(txn.DB(conn))), conn
}

func list(a *list_event_sections.Action, in list_event_sections.Input) (list_event_sections.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (list_event_sections.Output, error) {
		return a.Handle(ctx, in)
	})
}

func TestListsNewestFirstWithEventTitle(t *testing.T) {
	a, _ := newAction(t)
	out, err := list(a, list_event_sections.Input{EventID: 1, After: page.StartCursor, Limit: page.DefaultPageSize})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Sections) != 3 || out.Sections[0].Name != "Balcony" || out.Sections[0].Title != "Night Show" {
		t.Fatalf("out %+v", out)
	}
	if out.NextAfter != 0 {
		t.Fatalf("next_after %d", out.NextAfter)
	}
}

func TestF1_LimitOutOfRange(t *testing.T) {
	a, _ := newAction(t)
	if _, err := list(a, list_event_sections.Input{EventID: 1, After: page.StartCursor, Limit: 0}); !errors.Is(err, list_event_sections.F1) {
		t.Fatalf("want F1, got %v", err)
	}
}

func TestF2_AfterZero(t *testing.T) {
	a, _ := newAction(t)
	if _, err := list(a, list_event_sections.Input{EventID: 1, After: 0, Limit: 10}); !errors.Is(err, list_event_sections.F2) {
		t.Fatalf("want F2, got %v", err)
	}
}
