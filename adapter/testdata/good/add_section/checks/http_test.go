package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixtures/features/add_section"
	"example.com/fixtures/features/add_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

// identity stands in for the app's sign-in hook (test headers).
func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// serveOn wires the slice exactly like cmd/server/routes.go.
func serveOn(t *testing.T, conn *sql.DB, body, user, role string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(add_section.Route, httpx.Bind(add_section.Roles, add_section.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodPost, "/sections", strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorBody {
	t.Helper()
	var body httpx.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	return body
}

// Over HTTP against SQLite: organizer B (user 8) cannot add a section to
// organizer A's event 1 (HTTP 404, nothing written), nor by sending A's id
// as `user` (HTTP 400). Organizer A can (HTTP 201).
func TestF3_HTTPOrganizerBCannotAddToAsEvent(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, `{"event_id":1,"name":"Hijack","capacity":10}`, "8", "organizer")
	if eb := errorBody(t, rec); rec.Code != http.StatusNotFound || rec.Code != add_section.F3.Status || eb.Error.ID != add_section.F3.ID {
		t.Fatalf("B adding to A's event: status %d %s", rec.Code, rec.Body)
	}
	rec = serveOn(t, conn, `{"event_id":1,"name":"Hijack","capacity":10,"user":7}`, "8", "organizer")
	if eb := errorBody(t, rec); rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID {
		t.Fatalf("B sending user 7: status %d %s", rec.Code, rec.Body)
	}
	if got := sections(t, conn); len(got) != 0 {
		t.Fatalf("nothing may be written: %q", got)
	}
	rec = serveOn(t, conn, `{"event_id":1,"name":"Balcony","capacity":40}`, "7", "organizer")
	if rec.Code != http.StatusCreated {
		t.Fatalf("A adding to A's event: status %d %s", rec.Code, rec.Body)
	}
	if got := sections(t, conn); len(got) != 1 || got[0] != "1:Balcony:40" {
		t.Fatalf("sections %q", got)
	}
}

// Only organizers: 401 signed out, 403 for any other role (admins
// included); nothing is written.
func TestHTTPOnlyOrganizers(t *testing.T) {
	for _, tc := range []struct {
		user, role string
		want       httpx.Outcome
	}{
		{"", "", httpx.Unauthenticated},
		{"7", "customer", httpx.Forbidden},
		{"7", "admin", httpx.Forbidden},
	} {
		_, conn := newAction(t)
		rec := serveOn(t, conn, `{"event_id":1,"name":"x","capacity":1}`, tc.user, tc.role)
		if eb := errorBody(t, rec); rec.Code != tc.want.Status || eb.Error.ID != tc.want.ID {
			t.Errorf("%q %q: status %d %s", tc.user, tc.role, rec.Code, rec.Body)
		}
		if got := sections(t, conn); len(got) != 0 {
			t.Errorf("%q: nothing may be written: %q", tc.role, got)
		}
	}
}
