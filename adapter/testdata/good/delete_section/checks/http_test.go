package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixtures/features/delete_section"
	"example.com/fixtures/features/delete_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

func serveOn(t *testing.T, conn *sql.DB, body, user, role string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(delete_section.Route, httpx.Bind(delete_section.Roles, delete_section.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodDelete, "/sections", strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
	return rec
}

// Over HTTP against SQLite: organizer B deleting a section of organizer A's
// event gets F1 (HTTP 404) and nothing is deleted, also when B sends A's id
// as `user` (HTTP 400); organizer A deletes it (HTTP 200).
func TestF1_HTTPOrganizerBCannotDeleteAsSection(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, `{"section_id":1}`, "8", "organizer")
	var body httpx.ErrorBody
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != http.StatusNotFound || rec.Code != delete_section.F1.Status || body.Error.ID != delete_section.F1.ID {
		t.Fatalf("B deleting A's section: status %d %s", rec.Code, rec.Body)
	}
	rec = serveOn(t, conn, `{"section_id":1,"user":7}`, "8", "organizer")
	if rec.Code != httpx.BadInput.Status {
		t.Fatalf("B sending user 7: status %d %s", rec.Code, rec.Body)
	}
	if got := sections(t, conn); !same(got, untouched) {
		t.Fatalf("nothing may be deleted: %v", got)
	}
	rec = serveOn(t, conn, `{"section_id":1}`, "7", "organizer")
	if rec.Code != http.StatusOK {
		t.Fatalf("A deleting A's section: status %d %s", rec.Code, rec.Body)
	}
	if got := sections(t, conn); !same(got, []int64{2, 3}) {
		t.Fatalf("sections %v", got)
	}
}

// A section with seats: the delete fails (ON DELETE RESTRICT), HTTP 500
// with no detail, and nothing is deleted.
func TestHTTPSeatsRestrictTheDelete(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, `{"section_id":3}`, "7", "organizer")
	if rec.Code != httpx.Internal.Status {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if got := sections(t, conn); !same(got, untouched) {
		t.Fatalf("nothing may be deleted: %v", got)
	}
}

// Only organizers: 401 signed out, 403 for any other role (admins
// included: they use admin_delete_section); nothing is deleted.
func TestHTTPOnlyOrganizers(t *testing.T) {
	for _, tc := range []struct {
		user, role string
		code       int
	}{
		{"", "", http.StatusUnauthorized},
		{"7", "admin", http.StatusForbidden},
		{"7", "customer", http.StatusForbidden},
	} {
		_, conn := newAction(t)
		if rec := serveOn(t, conn, `{"section_id":1}`, tc.user, tc.role); rec.Code != tc.code {
			t.Errorf("%q: status %d %s", tc.role, rec.Code, rec.Body)
		}
		if got := sections(t, conn); !same(got, untouched) {
			t.Errorf("%q: nothing may be deleted: %v", tc.role, got)
		}
	}
}
