// Package checks holds the acceptance checks for Delete section.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/delete_section"
	"example.com/fixtures/features/delete_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer A (user 7) owns event 1 with sections 1 and 3 (section 3 has a
// seat); organizer B (user 8) owns event 2 with section 2.
const (
	organizerA = 7
	organizerB = 8
)

func newAction(t *testing.T) (*delete_section.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1), (2, 8, 'organizer', 'B''s event', 2);
		INSERT INTO sections (id, event_id, name, capacity) VALUES (1, 1, 'A floor', 100), (2, 2, 'B floor', 100), (3, 1, 'A balcony', 40);
		INSERT INTO section_seats (id, section_id, label) VALUES (1, 3, 'B1')`); err != nil {
		t.Fatal(err)
	}
	return delete_section.New(db.New(txn.DB(conn))), conn
}

func del(a *delete_section.Action, in delete_section.Input) (delete_section.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (delete_section.Output, error) { return a.Handle(ctx, in) })
}

// sections lists the ids of the sections left, in order.
func sections(t *testing.T, conn *sql.DB) []int64 {
	t.Helper()
	rows, err := conn.Query(`SELECT id FROM sections ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func same(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var untouched = []int64{1, 2, 3}

// "only sections of events you own (`events.organizer_id` is the signed-in
// user) can be deleted": organizer A deletes section 1.
func TestOwnerDeletesSectionOfOwnEvent(t *testing.T) {
	a, conn := newAction(t)
	out, err := del(a, delete_section.Input{SectionID: 1, User: organizerA})
	if err != nil || out.SectionID != 1 {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := sections(t, conn); !same(got, []int64{2, 3}) {
		t.Fatalf("sections %v", got)
	}
}

// F1: organizer B deleting A's section, and a section that does not exist,
// get the same F1; nothing is deleted.
func TestF1_NotYoursOrMissing(t *testing.T) {
	a, conn := newAction(t)
	for _, in := range []delete_section.Input{{SectionID: 1, User: organizerB}, {SectionID: 99, User: organizerA}} {
		if _, err := del(a, in); !errors.Is(err, delete_section.F1) {
			t.Fatalf("%+v: want F1, got %v", in, err)
		}
	}
	if got := sections(t, conn); !same(got, untouched) {
		t.Fatalf("F1 must delete nothing: %v", got)
	}
}

// "While a section seat has `section_id` equal to the `id` of a section
// being deleted, schema.sql refuses the delete (ON DELETE RESTRICT): the
// query fails and nothing is deleted."
func TestSeatsRestrictTheDelete(t *testing.T) {
	a, conn := newAction(t)
	_, err := del(a, delete_section.Input{SectionID: 3, User: organizerA})
	if err == nil || errors.Is(err, delete_section.F1) {
		t.Fatalf("want the query to fail, got %v", err)
	}
	if got := sections(t, conn); !same(got, untouched) {
		t.Fatalf("nothing may be deleted: %v", got)
	}
}
