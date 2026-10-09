// Package checks holds the acceptance checks for Admin rename section.
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
	"example.com/fixtures/features/admin_rename_section"
	"example.com/fixtures/features/admin_rename_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer 7 owns event 1 with section 1.
func newAction(t *testing.T) (*admin_rename_section.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'A''s event', 1);
		INSERT INTO sections (id, event_id, name, capacity) VALUES (1, 1, 'A floor', 100)`); err != nil {
		t.Fatal(err)
	}
	return admin_rename_section.New(db.New(txn.DB(conn))), conn
}

func rename(a *admin_rename_section.Action, in admin_rename_section.Input) (admin_rename_section.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (admin_rename_section.Output, error) { return a.Handle(ctx, in) })
}

func name(t *testing.T, conn *sql.DB) string {
	t.Helper()
	var n string
	if err := conn.QueryRow(`SELECT name FROM sections WHERE id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// "this step is not limited to sections of events you own": an admin
// renames a section of an event they do not organize.
func TestAdminRenamesAnyonesSection(t *testing.T) {
	a, conn := newAction(t)
	if out, err := rename(a, admin_rename_section.Input{SectionID: 1, Name: "Fixed"}); err != nil || out.Name != "Fixed" {
		t.Fatalf("out %+v err %v", out, err)
	}
	if got := name(t, conn); got != "Fixed" {
		t.Fatalf("name %q", got)
	}
}

func TestF1_EmptyName(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, admin_rename_section.Input{SectionID: 1, Name: ""}); !errors.Is(err, admin_rename_section.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if got := name(t, conn); got != "A floor" {
		t.Fatalf("F1 must write nothing: %q", got)
	}
}

func TestF2_NoSuchSection(t *testing.T) {
	a, conn := newAction(t)
	if _, err := rename(a, admin_rename_section.Input{SectionID: 99, Name: "Ghost"}); !errors.Is(err, admin_rename_section.F2) {
		t.Fatalf("want F2, got %v", err)
	}
	if got := name(t, conn); got != "A floor" {
		t.Fatalf("F2 must write nothing: %q", got)
	}
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// Over HTTP: only admins (the bypass role) may call it; the organizer who
// owns the event gets 403 here (they use rename_section), and nothing is
// written then.
func TestHTTPOnlyAdmins(t *testing.T) {
	for _, tc := range []struct {
		role, want string
		code       int
	}{
		{"admin", "Fixed", http.StatusOK},
		{"organizer", "A floor", http.StatusForbidden},
		{"customer", "A floor", http.StatusForbidden},
	} {
		_, conn := newAction(t)
		mux := http.NewServeMux()
		mux.Handle(admin_rename_section.Route, httpx.Bind(admin_rename_section.Roles, admin_rename_section.New(db.New(txn.DB(conn))).Handle))
		req := httptest.NewRequest(http.MethodPatch, "/admin/sections/name", strings.NewReader(`{"section_id":1,"name":"Fixed"}`))
		req.Header.Set("X-Test-User", "7")
		req.Header.Set("X-Test-Role", tc.role)
		rec := httptest.NewRecorder()
		httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
		var out admin_rename_section.Output
		if rec.Code != tc.code || (tc.code == http.StatusOK && (json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Name != "Fixed")) {
			t.Errorf("%s: status %d %s", tc.role, rec.Code, rec.Body)
		}
		if got := name(t, conn); got != tc.want {
			t.Errorf("%s: name %q, want %q", tc.role, got, tc.want)
		}
	}
}
