package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixtures/features/rename_event"
	"example.com/fixtures/features/rename_event/db"
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
func serveOn(t *testing.T, conn *sql.DB, target, body, user, role string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(rename_event.Route, httpx.Bind(rename_event.Roles, rename_event.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(body))
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

// Over HTTP against SQLite: organizer B (user 8) cannot change organizer
// A's event 1, also by sending its id, nor by sending A's id as `user`
// (HTTP 400); nothing is written. Organizer A can.
func TestF2_HTTPOrganizerBCannotRenameAsEvent(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, "/events/1/title", `{"title":"Hijacked"}`, "8", "organizer")
	if eb := errorBody(t, rec); rec.Code != rename_event.F2.Status || eb.Error.ID != rename_event.F2.ID {
		t.Fatalf("B renaming A's event: status %d %s", rec.Code, rec.Body)
	}
	rec = serveOn(t, conn, "/events/1/title", `{"title":"Hijacked","user":7}`, "8", "organizer")
	if eb := errorBody(t, rec); rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID {
		t.Fatalf("B sending user 7: status %d %s", rec.Code, rec.Body)
	}
	rec = serveOn(t, conn, "/events/1/title", `{"event_id":1,"title":"Hijacked"}`, "8", "organizer")
	if eb := errorBody(t, rec); rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID {
		t.Fatalf("body carrying path field event_id: status %d %s", rec.Code, rec.Body)
	}
	if got := titles(t, conn); got != untouched {
		t.Fatalf("nothing may be written: %q", got)
	}
	rec = serveOn(t, conn, "/events/1/title", `{"title":"Launch"}`, "7", "organizer")
	if rec.Code != http.StatusOK {
		t.Fatalf("A renaming A's event: status %d %s", rec.Code, rec.Body)
	}
	if got := titles(t, conn); got != [2]string{"Launch", "B's event"} {
		t.Fatalf("titles %q", got)
	}
}

// Only organizers: 401 signed out, 403 for any other role, admins included
// (they use admin_rename_event); nothing is written.
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
		rec := serveOn(t, conn, "/events/1/title", `{"title":"x"}`, tc.user, tc.role)
		if eb := errorBody(t, rec); rec.Code != tc.want.Status || eb.Error.ID != tc.want.ID {
			t.Errorf("%q %q: status %d %s", tc.user, tc.role, rec.Code, rec.Body)
		}
		if got := titles(t, conn); got != untouched {
			t.Errorf("%q: nothing may be written: %q", tc.role, got)
		}
	}
}
