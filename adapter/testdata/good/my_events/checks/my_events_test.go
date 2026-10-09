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

func newConn(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (organizer_id, created_as, title, starts_at) VALUES (7, 'organizer', 'a', 1), (7, 'organizer', 'b', 2), (8, 'admin', 'c', 3)`); err != nil {
		t.Fatal(err)
	}
	return conn
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin")

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

// "Who may call it: anyone, signed in or not." Signed out, the user is 0
// and the role the empty text (no event has organizer 0); signed in, they
// are the signed-in user's, in any role.
func TestHTTPAnyoneMayCall(t *testing.T) {
	conn := newConn(t)
	for _, tc := range []struct {
		user, role string
		want       my_events.Output
	}{
		{"", "", my_events.Output{Events: 0, Role: ""}},
		{"7", "organizer", my_events.Output{Events: 2, Role: "organizer"}},
		{"8", "customer", my_events.Output{Events: 1, Role: "customer"}},
		{"9", "finance", my_events.Output{Events: 0, Role: "finance"}},
		{"7", "superuser", my_events.Output{Events: 0, Role: ""}}, // a role the app does not declare: not signed in
	} {
		rec := serve(t, conn, "/me/events", tc.user, tc.role)
		var out my_events.Output
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out != tc.want {
			t.Errorf("user %q role %q: status %d %s; want %+v", tc.user, tc.role, rec.Code, rec.Body, tc.want)
		}
	}
}

// The user and role never come from the query string: 400, also signed out.
func TestHTTPUserOrRoleInTheQueryIs400(t *testing.T) {
	conn := newConn(t)
	for _, target := range []string{"/me/events?user=7", "/me/events?role=admin", "/me/events?USER=7"} {
		for _, user := range []string{"", "8"} {
			rec := serve(t, conn, target, user, "customer")
			var body httpx.ErrorBody
			if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != httpx.BadInput.Status || body.Error.ID != httpx.BadInput.ID {
				t.Errorf("%s (user %q): status %d %s", target, user, rec.Code, rec.Body)
			}
		}
	}
}
