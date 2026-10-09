// Package checks holds the acceptance checks for Admin rename event.
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
	"example.com/fixtures/features/admin_rename_event"
	"example.com/fixtures/features/admin_rename_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func newAction(t *testing.T) (*admin_rename_event.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1)`); err != nil {
		t.Fatal(err)
	}
	return admin_rename_event.New(db.New(txn.DB(conn))), conn
}

func rename(a *admin_rename_event.Action, in admin_rename_event.Input) (admin_rename_event.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (admin_rename_event.Output, error) { return a.Handle(ctx, in) })
}

func title(t *testing.T, conn *sql.DB) string {
	t.Helper()
	var s string
	if err := conn.QueryRow(`SELECT title FROM events WHERE id = 1`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// "this step is not limited to events you own": the admin renames
// organizer A's event.
func TestAdminRenamesAnyonesEvent(t *testing.T) {
	a, conn := newAction(t)
	if out, err := rename(a, admin_rename_event.Input{EventID: 1, Title: "Fixed"}); err != nil || out.Title != "Fixed" {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := title(t, conn); got != "Fixed" {
		t.Fatalf("title %q", got)
	}
}

func TestF1_EmptyTitle(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, admin_rename_event.Input{EventID: 1, Title: ""}); !errors.Is(err, admin_rename_event.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if got := title(t, conn); got != "A's event" {
		t.Fatalf("F1 must write nothing: %q", got)
	}
}

func TestF2_NoSuchEvent(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, admin_rename_event.Input{EventID: 99, Title: "Ghost"}); !errors.Is(err, admin_rename_event.F2) {
		t.Fatalf("want F2, got %v", err)
	}
	if got := title(t, conn); got != "A's event" {
		t.Fatalf("F2 must write nothing: %q", got)
	}
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

// Over HTTP: an admin renames organizer A's event; an organizer (even A,
// the owner) is answered 403 and nothing is written.
func TestHTTPOnlyAdmins(t *testing.T) {
	for _, tc := range []struct {
		role, want string
		code       int
	}{
		{"admin", "Fixed", http.StatusOK},
		{"organizer", "A's event", http.StatusForbidden},
	} {
		_, conn := newAction(t)
		mux := http.NewServeMux()
		mux.Handle(admin_rename_event.Route, httpx.Bind(admin_rename_event.Roles, admin_rename_event.New(db.New(txn.DB(conn))).Handle))
		identity := func(*http.Request) (string, string, bool) { return "7", tc.role, true }
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/admin/events/title", strings.NewReader(`{"event_id":1,"title":"Fixed"}`))
		httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%s: status %d %s", tc.role, rec.Code, rec.Body)
		}
		var out admin_rename_event.Output
		if tc.code == http.StatusOK && (json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Title != "Fixed") {
			t.Errorf("%s: body %s", tc.role, rec.Body)
		}
		if got := title(t, conn); got != tc.want {
			t.Errorf("%s: title %q, want %q", tc.role, got, tc.want)
		}
	}
}
