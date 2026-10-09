package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/fixtures/features/create_event"
	"example.com/fixtures/features/create_event/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

// identity stands in for the app's sign-in hook: the signed-in user and role
// come from the test headers (a real app reads its own session cookie and
// store; bridge-en never sees passwords or sessions).
func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// serveOn wires the slice exactly like cmd/server/routes.go: Bind with the
// slice's Roles, behind httpx.Identify with the app's roles.
func serveOn(t *testing.T, conn *sql.DB, target, body, user, role string) *httptest.ResponseRecorder {
	t.Helper()
	httpx.Now = func() time.Time { return time.Unix(t0, 0) }
	t.Cleanup(func() { httpx.Now = time.Now })
	mux := http.NewServeMux()
	mux.Handle(create_event.Route, httpx.Bind(create_event.Roles, create_event.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
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

const goodBody = `{"title":"Launch","starts_at":1800000600}`

// "Who may call it: signed-in users with role `organizer` or `admin`.
// Anyone else is answered with HTTP 403 (HTTP 401 if not signed in), and
// the action does not run." Not signed in (no sign-in at all, or a role the
// app does not declare): 401. Signed in as a customer or finance: 403.
// Neither writes anything, also with a body that would have failed.
func TestHTTPOnlyOrganizersAndAdmins(t *testing.T) {
	cases := []struct {
		name, user, role string
		want             httpx.Outcome
	}{
		{"not signed in", "", "", httpx.Unauthenticated},
		{"undeclared role", "7", "superuser", httpx.Unauthenticated},
		{"user id not a number", "abc", "organizer", httpx.Unauthenticated},
		{"customer", "7", "customer", httpx.Forbidden},
		{"finance", "7", "finance", httpx.Forbidden},
	}
	for _, tc := range cases {
		for _, body := range []string{goodBody, `{"title":""}`, `{"title":"x","starts_at":1800000600,"user":7}`} {
			_, conn := newAction(t)
			rec := serveOn(t, conn, "/events", body, tc.user, tc.role)
			eb := errorBody(t, rec)
			if rec.Code != tc.want.Status || eb.Error.ID != tc.want.ID || eb.Error.Message != tc.want.Message {
				t.Errorf("%s, %s: status %d %s; want %d %q", tc.name, body, rec.Code, rec.Body, tc.want.Status, tc.want.ID)
			}
			if n := countEvents(t, conn); n != 0 {
				t.Errorf("%s: the action ran (%d events written)", tc.name, n)
			}
		}
	}
	if httpx.Unauthenticated.Status != http.StatusUnauthorized || httpx.Forbidden.Status != http.StatusForbidden {
		t.Fatal("401 / 403")
	}
}

// The allowed roles run the action, and the event belongs to the signed-in
// user from the server.
func TestHTTPOrganizerAndAdminCreate(t *testing.T) {
	for _, role := range []string{"organizer", "admin"} {
		_, conn := newAction(t)
		rec := serveOn(t, conn, "/events", goodBody, "42", role)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: status %d %s", role, rec.Code, rec.Body)
		}
		var out create_event.Output
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.OrganizerID != 42 || out.CreatedAs != role {
			t.Fatalf("%s: %s (%v)", role, rec.Body, err)
		}
		if countEvents(t, conn) != 1 {
			t.Fatalf("%s: want one event", role)
		}
	}
}

// F2 over HTTP: the server's clock is the current time (starts_at = now).
func TestF2_HTTPStartsNow(t *testing.T) {
	_, conn := newAction(t)
	rec := serveOn(t, conn, "/events", `{"title":"Now","starts_at":1800000000}`, "42", "organizer")
	if eb := errorBody(t, rec); rec.Code != create_event.F2.Status || eb.Error.ID != create_event.F2.ID {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if countEvents(t, conn) != 0 {
		t.Fatal("F2 must write nothing")
	}
}

// The user and role are never taken from the request: an allowed caller
// who sends `user` or `role` in the body or the query string, in any letter
// case, gets HTTP 400 and nothing is written.
func TestHTTPUserAndRoleFromTheRequestAre400(t *testing.T) {
	for _, c := range []struct{ target, body string }{
		{"/events", `{"title":"x","starts_at":1800000600,"user":1}`},
		{"/events", `{"title":"x","starts_at":1800000600,"User":1}`},
		{"/events", `{"title":"x","starts_at":1800000600,"role":"admin"}`},
		{"/events?user=1", goodBody},
		{"/events?role=admin", goodBody},
	} {
		_, conn := newAction(t)
		rec := serveOn(t, conn, c.target, c.body, "42", "organizer")
		if eb := errorBody(t, rec); rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID || !strings.Contains(eb.Error.Message, "is set by the server") {
			t.Errorf("%s %s: status %d %s", c.target, c.body, rec.Code, rec.Body)
		}
		if countEvents(t, conn) != 0 {
			t.Errorf("%s %s: a bad request must not write", c.target, c.body)
		}
	}
}

// "Ownership: the new event is yours (`organizer_id` is the signed-in
// user)." events is owned by organizer_id (A4, schema.sql), so the stored
// row's owner is the signed-in user from the server for every allowed role
// (admin too: this action is also open to organizers, so it never bypasses
// ownership), and an owner sent in the body is HTTP 400 with nothing written.
func TestHTTPNewEventIsOwnedByTheSignedInUser(t *testing.T) {
	for _, role := range []string{"organizer", "admin"} {
		_, conn := newAction(t)
		if rec := serveOn(t, conn, "/events", goodBody, "42", role); rec.Code != http.StatusCreated {
			t.Fatalf("%s: status %d %s", role, rec.Code, rec.Body)
		}
		var owner int64
		if err := conn.QueryRow("SELECT organizer_id FROM events").Scan(&owner); err != nil || owner != 42 {
			t.Fatalf("%s: stored organizer_id %d (%v), want the signed-in user 42", role, owner, err)
		}
	}
	_, conn := newAction(t)
	rec := serveOn(t, conn, "/events", `{"title":"x","starts_at":1800000600,"organizer_id":7}`, "42", "organizer")
	if eb := errorBody(t, rec); rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID {
		t.Fatalf("an owner in the body: status %d %s", rec.Code, rec.Body)
	}
	if countEvents(t, conn) != 0 {
		t.Fatal("an owner in the body must not write")
	}
}
