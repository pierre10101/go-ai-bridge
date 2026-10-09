// Package checks holds the acceptance checks for My events (no failure
// cases: intent.md says "None.").
package checks

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/my_events"
	"example.com/fixtures/features/my_events/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer 7 owns events 1 and 2 (three sections), organizer 8 owns event
// 3 (one section).
func newConn(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'a', 1), (2, 7, 'organizer', 'b', 2), (3, 8, 'admin', 'c', 3);
		INSERT INTO sections (event_id, name, capacity) VALUES (1, 'Floor', 100), (1, 'Balcony', 40), (2, 'Floor', 80), (3, 'Floor', 60)`); err != nil {
		t.Fatal(err)
	}
	return conn
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

func identity(r *http.Request) (string, string, bool) {
	user := r.Header.Get("X-Test-User")
	return user, r.Header.Get("X-Test-Role"), user != ""
}

// serve wires the slice exactly like cmd/server/routes.go.
func serve(t *testing.T, conn *sql.DB, target, user, role string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(my_events.Route, httpx.Bind(my_events.Roles, my_events.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
	return rec
}

// "only events you own (`organizer_id` is the signed-in user) are read" and
// "only sections of events you own (`events.organizer_id` is the signed-in
// user) are read": each organizer counts only their own.
func TestHTTPOrganizerCountsOnlyOwnEventsAndSections(t *testing.T) {
	conn := newConn(t)
	for _, tc := range []struct {
		user string
		want my_events.Output
	}{
		{"7", my_events.Output{Events: 2, Sections: 3}},
		{"8", my_events.Output{Events: 1, Sections: 1}},
		{"9", my_events.Output{Events: 0, Sections: 0}},
	} {
		rec := serve(t, conn, "/me/events", tc.user, "organizer")
		var out my_events.Output
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out != tc.want {
			t.Errorf("user %q: status %d %s; want %+v", tc.user, rec.Code, rec.Body, tc.want)
		}
	}
}

// "Who may call it: signed-in users with role `organizer`." Signed out: 401
// (there is no "user 0" answer); any other role: 403.
func TestHTTPOnlyOrganizers(t *testing.T) {
	conn := newConn(t)
	for _, tc := range []struct {
		user, role string
		want       httpx.Outcome
	}{
		{"", "", httpx.Unauthenticated},
		{"7", "superuser", httpx.Unauthenticated}, // a role the app does not declare: not signed in
		{"7", "customer", httpx.Forbidden},
		{"7", "admin", httpx.Forbidden},
	} {
		rec := serve(t, conn, "/me/events", tc.user, tc.role)
		var body httpx.ErrorBody
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != tc.want.Status || body.Error.ID != tc.want.ID {
			t.Errorf("user %q role %q: status %d %s", tc.user, tc.role, rec.Code, rec.Body)
		}
	}
}

// The user never comes from the query string: 400.
func TestHTTPUserInTheQueryIs400(t *testing.T) {
	conn := newConn(t)
	for _, target := range []string{"/me/events?user=8", "/me/events?USER=8"} {
		rec := serve(t, conn, target, "7", "organizer")
		var body httpx.ErrorBody
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != httpx.BadInput.Status || body.Error.ID != httpx.BadInput.ID {
			t.Errorf("%s: status %d %s", target, rec.Code, rec.Body)
		}
	}
}

// T4: "The query string is as strict as a body: a query parameter that is
// not listed above (names are case-sensitive) is answered with HTTP 400".
// My events lists none, so any query parameter is a bad request.
func TestHTTPUnknownQueryParameterIs400(t *testing.T) {
	conn := newConn(t)
	for _, target := range []string{"/me/events?x=1", "/me/events?organizer_id=8", "/me/events?limit=5"} {
		rec := serve(t, conn, target, "7", "organizer")
		var body httpx.ErrorBody
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != httpx.BadInput.Status || body.Error.ID != httpx.BadInput.ID {
			t.Errorf("%s: status %d %s", target, rec.Code, rec.Body)
		}
	}
}
