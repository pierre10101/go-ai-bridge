// Package checks holds the acceptance checks for Delete event.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/delete_event"
	"example.com/fixtures/features/delete_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer A (user 7) owns event 1 (sections 1 and 2) and event 3
// (section 4, which has a seat); organizer B (user 8) owns event 2
// (section 3).
const (
	organizerA = 7
	organizerB = 8
)

func newAction(t *testing.T) (*delete_event.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1), (2, 8, 'organizer', 'B''s event', 2), (3, 7, 'organizer', 'A''s gala', 3);
		INSERT INTO sections (id, event_id, name, capacity) VALUES (1, 1, 'A floor', 100), (2, 1, 'A balcony', 40), (3, 2, 'B floor', 100), (4, 3, 'Gala floor', 10);
		INSERT INTO section_seats (id, section_id, label) VALUES (1, 4, 'G1')`); err != nil {
		t.Fatal(err)
	}
	return delete_event.New(db.New(txn.DB(conn))), conn
}

func del(a *delete_event.Action, in delete_event.Input) (delete_event.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (delete_event.Output, error) { return a.Handle(ctx, in) })
}

// left is the number of events and of sections left.
func left(t *testing.T, conn *sql.DB) [2]int {
	t.Helper()
	var n [2]int
	if err := conn.QueryRow(`SELECT (SELECT COUNT(*) FROM events), (SELECT COUNT(*) FROM sections)`).Scan(&n[0], &n[1]); err != nil {
		t.Fatal(err)
	}
	return n
}

var untouched = [2]int{3, 4}

// "only events you own ... can be deleted by this step" and "Each section
// whose `event_id` is the `id` of a deleted event is deleted with it":
// organizer A deletes event 1 and its two sections go with it.
func TestOwnerDeletesOwnEventWithItsSections(t *testing.T) {
	a, conn := newAction(t)
	if out, err := del(a, delete_event.Input{EventID: 1, User: organizerA}); err != nil || out.EventID != 1 {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := left(t, conn); got != [2]int{2, 2} {
		t.Fatalf("events, sections left: %v", got)
	}
}

func TestF1_NotYoursOrMissing(t *testing.T) {
	a, conn := newAction(t)
	for _, in := range []delete_event.Input{{EventID: 1, User: organizerB}, {EventID: 99, User: organizerA}} {
		if _, err := del(a, in); !errors.Is(err, delete_event.F1) {
			t.Fatalf("%+v: want F1, got %v", in, err)
		}
	}
	if got := left(t, conn); got != untouched {
		t.Fatalf("F1 must delete nothing: %v", got)
	}
}

// "While a section seat has `section_id` equal to the `id` of a section
// being deleted, schema.sql refuses the delete": event 3's section has a
// seat, so the query fails and neither the event nor its section is
// deleted.
func TestSeatsRestrictTheCascade(t *testing.T) {
	a, conn := newAction(t)
	if _, err := del(a, delete_event.Input{EventID: 3, User: organizerA}); err == nil || errors.Is(err, delete_event.F1) {
		t.Fatalf("want the query to fail, got %v", err)
	}
	if got := left(t, conn); got != untouched {
		t.Fatalf("nothing may be deleted: %v", got)
	}
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// Over HTTP against SQLite: organizer B deleting organizer A's event gets
// F1 (HTTP 404) and nothing is deleted; A deletes it (HTTP 200).
func TestF1_HTTPOrganizerBCannotDeleteAsEvent(t *testing.T) {
	_, conn := newAction(t)
	serve := func(user string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		mux.Handle(delete_event.Route, httpx.Bind(delete_event.Roles, delete_event.New(db.New(txn.DB(conn))).Handle))
		req := httptest.NewRequest(http.MethodDelete, "/events", strings.NewReader(`{"event_id":1}`))
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", "organizer")
		rec := httptest.NewRecorder()
		httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
		return rec
	}
	rec := serve("8")
	var body httpx.ErrorBody
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != http.StatusNotFound || body.Error.ID != delete_event.F1.ID {
		t.Fatalf("B deleting A's event: status %d %s", rec.Code, rec.Body)
	}
	if got := left(t, conn); got != untouched {
		t.Fatalf("nothing may be deleted: %v", got)
	}
	if rec := serve("7"); rec.Code != http.StatusOK {
		t.Fatalf("A deleting A's event: status %d %s", rec.Code, rec.Body)
	}
	if got := left(t, conn); got != [2]int{2, 2} {
		t.Fatalf("events, sections left: %v", got)
	}
}
