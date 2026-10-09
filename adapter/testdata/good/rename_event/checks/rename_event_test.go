// Package checks holds the acceptance checks for Rename event.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/rename_event"
	"example.com/fixtures/features/rename_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer A (user 7) owns event 1; organizer B (user 8) owns event 2.
const (
	organizerA = 7
	organizerB = 8
)

func newAction(t *testing.T) (*rename_event.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1), (2, 8, 'organizer', 'B''s event', 2)`); err != nil {
		t.Fatal(err)
	}
	return rename_event.New(db.New(txn.DB(conn))), conn
}

// rename calls the action the way httpx.Bind does once the caller is
// allowed: in one transaction, with the signed-in user set by the server.
func rename(a *rename_event.Action, in rename_event.Input) (rename_event.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (rename_event.Output, error) { return a.Handle(ctx, in) })
}

func titles(t *testing.T, conn *sql.DB) [2]string {
	t.Helper()
	var out [2]string
	for i := range out {
		if err := conn.QueryRow(`SELECT title FROM events WHERE id = ?`, i+1).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

var untouched = [2]string{"A's event", "B's event"}

// "only events you own (`organizer_id` is the signed-in user) can be
// changed": organizer A renames event 1.
func TestOwnerRenamesOwnEvent(t *testing.T) {
	a, conn := newAction(t)
	out, err := rename(a, rename_event.Input{EventID: 1, Title: "Launch", User: organizerA})
	if err != nil || out != (rename_event.Output{EventID: 1, Title: "Launch"}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := titles(t, conn); got != [2]string{"Launch", "B's event"} {
		t.Fatalf("titles %q", got)
	}
}

func TestF1_EmptyTitle(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, rename_event.Input{EventID: 1, Title: "", User: organizerA}); !errors.Is(err, rename_event.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if got := titles(t, conn); got != untouched {
		t.Fatalf("F1 must write nothing: %q", got)
	}
}

// F2: organizer B cannot rename organizer A's event (nothing is written),
// and the answer is the same as for an event that does not exist.
func TestF2_NotYoursOrMissing(t *testing.T) {
	a, conn := newAction(t)
	for _, in := range []rename_event.Input{
		{EventID: 1, Title: "Hijacked", User: organizerB}, // A's event, B signed in
		{EventID: 2, Title: "Hijacked", User: organizerA}, // B's event, A signed in
		{EventID: 99, Title: "Ghost", User: organizerA},   // no such event
		{EventID: 1, Title: "Signed out", User: 0},        // never happens (Roles), still nothing
	} {
		if _, err := rename(a, in); !errors.Is(err, rename_event.F2) {
			t.Fatalf("%+v: want F2, got %v", in, err)
		}
	}
	if got := titles(t, conn); got != untouched {
		t.Fatalf("F2 must write nothing: %q", got)
	}
}
