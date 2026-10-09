// Package checks holds the acceptance checks for Add section.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/add_section"
	"example.com/fixtures/features/add_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer A (user 7) owns event 1; organizer B (user 8) owns event 2.
const (
	organizerA = 7
	organizerB = 8
)

func newAction(t *testing.T) (*add_section.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1), (2, 8, 'organizer', 'B''s event', 2)`); err != nil {
		t.Fatal(err)
	}
	return add_section.New(db.New(txn.DB(conn))), conn
}

// add calls the action the way httpx.Bind does once the caller is allowed:
// in one transaction, with the signed-in user set by the server.
func add(a *add_section.Action, in add_section.Input) (add_section.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (add_section.Output, error) { return a.Handle(ctx, in) })
}

// sections lists every section as "<event_id>:<name>:<capacity>".
func sections(t *testing.T, conn *sql.DB) []string {
	t.Helper()
	rows, err := conn.Query(`SELECT event_id || ':' || name || ':' || capacity FROM sections ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// "the new section is added only to an event you own": organizer A adds a
// section to event 1.
func TestOwnerAddsSectionToOwnEvent(t *testing.T) {
	a, conn := newAction(t)
	out, err := add(a, add_section.Input{EventID: 1, Name: "Balcony", Capacity: 40, User: organizerA})
	if err != nil || out != (add_section.Output{EventID: 1, Name: "Balcony"}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := sections(t, conn); len(got) != 1 || got[0] != "1:Balcony:40" {
		t.Fatalf("sections %q", got)
	}
}

func TestF1_EmptyName(t *testing.T) {
	a, conn := newAction(t)
	if _, err := add(a, add_section.Input{EventID: 1, Name: "", Capacity: 40, User: organizerA}); !errors.Is(err, add_section.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if got := sections(t, conn); len(got) != 0 {
		t.Fatalf("F1 must write nothing: %q", got)
	}
}

func TestF2_CapacityBelowOne(t *testing.T) {
	a, conn := newAction(t)
	for _, c := range []int64{0, -5} {
		if _, err := add(a, add_section.Input{EventID: 1, Name: "Pit", Capacity: c, User: organizerA}); !errors.Is(err, add_section.F2) {
			t.Fatalf("capacity %d: want F2, got %v", c, err)
		}
	}
	if got := sections(t, conn); len(got) != 0 {
		t.Fatalf("F2 must write nothing: %q", got)
	}
}

// F3: "for any other event nothing is written": organizer B cannot add a
// section to organizer A's event, and the answer is the same as for an
// event that does not exist.
func TestF3_NotYoursOrMissing(t *testing.T) {
	a, conn := newAction(t)
	for _, in := range []add_section.Input{
		{EventID: 1, Name: "Hijack", Capacity: 10, User: organizerB}, // A's event, B signed in
		{EventID: 2, Name: "Hijack", Capacity: 10, User: organizerA}, // B's event, A signed in
		{EventID: 99, Name: "Ghost", Capacity: 10, User: organizerA}, // no such event
		{EventID: 1, Name: "Signed out", Capacity: 10, User: 0},      // never happens (Roles), still nothing
	} {
		if _, err := add(a, in); !errors.Is(err, add_section.F3) {
			t.Fatalf("%+v: want F3, got %v", in, err)
		}
	}
	if got := sections(t, conn); len(got) != 0 {
		t.Fatalf("F3 must write nothing: %q", got)
	}
}
