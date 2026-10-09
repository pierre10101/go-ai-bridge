// Package checks holds the acceptance checks for Rename section.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/rename_section"
	"example.com/fixtures/features/rename_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer A (user 7) owns event 1 with section 1; organizer B (user 8)
// owns event 2 with section 2.
const (
	organizerA = 7
	organizerB = 8
)

func newAction(t *testing.T) (*rename_section.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1), (2, 8, 'organizer', 'B''s event', 2);
		INSERT INTO sections (id, event_id, name, capacity) VALUES (1, 1, 'A floor', 100), (2, 2, 'B floor', 100)`); err != nil {
		t.Fatal(err)
	}
	return rename_section.New(db.New(txn.DB(conn))), conn
}

func rename(a *rename_section.Action, in rename_section.Input) (rename_section.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (rename_section.Output, error) { return a.Handle(ctx, in) })
}

func names(t *testing.T, conn *sql.DB) [2]string {
	t.Helper()
	var out [2]string
	for i := range out {
		if err := conn.QueryRow(`SELECT name FROM sections WHERE id = ?`, i+1).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

var untouched = [2]string{"A floor", "B floor"}

// "only sections of events you own (`events.organizer_id` is the signed-in
// user) can be changed": organizer A renames section 1.
func TestOwnerRenamesSectionOfOwnEvent(t *testing.T) {
	a, conn := newAction(t)
	out, err := rename(a, rename_section.Input{SectionID: 1, Name: "Stalls", User: organizerA})
	if err != nil || out != (rename_section.Output{SectionID: 1, Name: "Stalls"}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := names(t, conn); got != [2]string{"Stalls", "B floor"} {
		t.Fatalf("names %q", got)
	}
}

func TestF1_EmptyName(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, rename_section.Input{SectionID: 1, Name: "", User: organizerA}); !errors.Is(err, rename_section.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if got := names(t, conn); got != untouched {
		t.Fatalf("F1 must write nothing: %q", got)
	}
}

// F2: organizer B cannot rename a section of organizer A's event (nothing
// is written), and the answer is the same as for a missing section.
func TestF2_NotYoursOrMissing(t *testing.T) {
	a, conn := newAction(t)
	for _, in := range []rename_section.Input{
		{SectionID: 1, Name: "Hijacked", User: organizerB},
		{SectionID: 2, Name: "Hijacked", User: organizerA},
		{SectionID: 99, Name: "Ghost", User: organizerA},
		{SectionID: 1, Name: "Signed out", User: 0},
	} {
		if _, err := rename(a, in); !errors.Is(err, rename_section.F2) {
			t.Fatalf("%+v: want F2, got %v", in, err)
		}
	}
	if got := names(t, conn); got != untouched {
		t.Fatalf("F2 must write nothing: %q", got)
	}
}
