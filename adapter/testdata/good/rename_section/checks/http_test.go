package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixtures/features/rename_section"
	"example.com/fixtures/features/rename_section/db"
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
	mux.Handle(rename_section.Route, httpx.Bind(rename_section.Roles, rename_section.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodPatch, "/sections/name", strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
	return rec
}

// Over HTTP against SQLite: organizer B cannot rename the section of
// organizer A's event (HTTP 404, nothing written); organizer A can.
func TestF2_HTTPOrganizerBCannotRenameAsSection(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, `{"section_id":1,"name":"Hijacked"}`, "8", "organizer")
	var body httpx.ErrorBody
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != rename_section.F2.Status || body.Error.ID != rename_section.F2.ID {
		t.Fatalf("B renaming A's section: status %d %s", rec.Code, rec.Body)
	}
	if got := names(t, conn); got != untouched {
		t.Fatalf("nothing may be written: %q", got)
	}
	rec = serveOn(t, conn, `{"section_id":1,"name":"Stalls"}`, "7", "organizer")
	if rec.Code != http.StatusOK {
		t.Fatalf("A renaming A's section: status %d %s", rec.Code, rec.Body)
	}
	if got := names(t, conn); got != [2]string{"Stalls", "B floor"} {
		t.Fatalf("names %q", got)
	}
}
