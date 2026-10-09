// Package checks holds the acceptance checks for Admin delete section.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/admin_delete_section"
	"example.com/fixtures/features/admin_delete_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer 7 owns event 1 with section 1.
func newAction(t *testing.T) (*admin_delete_section.Action, *sql.DB) {
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
	return admin_delete_section.New(db.New(txn.DB(conn))), conn
}

func del(a *admin_delete_section.Action, in admin_delete_section.Input) (admin_delete_section.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (admin_delete_section.Output, error) { return a.Handle(ctx, in) })
}

func count(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sections`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// "this step is not limited to sections of events you own": an admin
// deletes a section of an event they do not organize.
func TestAdminDeletesAnyonesSection(t *testing.T) {
	a, conn := newAction(t)
	if out, err := del(a, admin_delete_section.Input{SectionID: 1}); err != nil || out.SectionID != 1 {
		t.Fatalf("out %+v err %v", out, err)
	}
	if n := count(t, conn); n != 0 {
		t.Fatalf("sections left: %d", n)
	}
}

func TestF1_NoSuchSection(t *testing.T) {
	a, conn := newAction(t)
	if _, err := del(a, admin_delete_section.Input{SectionID: 99}); !errors.Is(err, admin_delete_section.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if n := count(t, conn); n != 1 {
		t.Fatalf("F1 must delete nothing: %d", n)
	}
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// Over HTTP: only admins (the bypass role) may call it; the organizer who
// owns the event gets 403 here (they use delete_section), and nothing is
// deleted then.
func TestHTTPOnlyAdmins(t *testing.T) {
	for _, tc := range []struct {
		role string
		code int
		left int
	}{
		{"admin", http.StatusOK, 0},
		{"organizer", http.StatusForbidden, 1},
		{"customer", http.StatusForbidden, 1},
	} {
		_, conn := newAction(t)
		mux := http.NewServeMux()
		mux.Handle(admin_delete_section.Route, httpx.Bind(admin_delete_section.Roles, admin_delete_section.New(db.New(txn.DB(conn))).Handle))
		req := httptest.NewRequest(http.MethodDelete, "/admin/sections", strings.NewReader(`{"section_id":1}`))
		req.Header.Set("X-Test-User", "7")
		req.Header.Set("X-Test-Role", tc.role)
		rec := httptest.NewRecorder()
		httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%s: status %d %s", tc.role, rec.Code, rec.Body)
		}
		if n := count(t, conn); n != tc.left {
			t.Errorf("%s: sections left %d, want %d", tc.role, n, tc.left)
		}
	}
}
