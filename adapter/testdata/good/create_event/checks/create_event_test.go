// Package checks holds the acceptance checks for Create event.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/create_event"
	"example.com/fixtures/features/create_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

const t0 = 1_800_000_000 // any time: checks pass the current time in (T1)

func newAction(t *testing.T) (*create_event.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return create_event.New(db.New(txn.DB(conn))), conn
}

// create calls the action the way httpx.Bind does once the caller is
// allowed: in one transaction, with the signed-in user, role and time set
// by the server (T1, T3).
func create(a *create_event.Action, in create_event.Input) (create_event.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (create_event.Output, error) { return a.Handle(ctx, in) })
}

func countEvents(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreatesTheSignedInUsersEvent(t *testing.T) {
	a, conn := newAction(t)
	out, err := create(a, create_event.Input{Title: "Launch", StartsAt: t0 + 1, User: 7, Role: "organizer", Now: t0})
	if err != nil || out.OrganizerID != 7 || out.CreatedAs != "organizer" || out.EventID == 0 {
		t.Fatalf("out %+v err %v", out, err)
	}
	var organizer int64
	var role string
	if err := conn.QueryRow(`SELECT organizer_id, created_as FROM events WHERE id = ?`, out.EventID).Scan(&organizer, &role); err != nil || organizer != 7 || role != "organizer" {
		t.Fatalf("stored organizer %d as %q (%v)", organizer, role, err)
	}
}

func TestF1_EmptyTitle(t *testing.T) {
	a, conn := newAction(t)
	_, err := create(a, create_event.Input{Title: "", StartsAt: t0 + 60, User: 7, Role: "organizer", Now: t0})
	if !errors.Is(err, create_event.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if countEvents(t, conn) != 0 {
		t.Fatal("F1 must write nothing")
	}
}

// The English: "`starts_at` is no later than the current time". An event
// that starts exactly now, or earlier, is refused; one second later is not.
func TestF2_StartsNowOrEarlier(t *testing.T) {
	a, conn := newAction(t)
	for _, starts := range []int64{t0, t0 - 1, 0} {
		_, err := create(a, create_event.Input{Title: "Late", StartsAt: starts, User: 7, Role: "admin", Now: t0})
		if !errors.Is(err, create_event.F2) {
			t.Fatalf("starts_at %d: want F2, got %v", starts, err)
		}
	}
	if countEvents(t, conn) != 0 {
		t.Fatal("F2 must write nothing")
	}
	if _, err := create(a, create_event.Input{Title: "On time", StartsAt: t0 + 1, User: 7, Role: "admin", Now: t0}); err != nil {
		t.Fatalf("one second after now: %v", err)
	}
}
