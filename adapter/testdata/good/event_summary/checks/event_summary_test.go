// Package checks holds the acceptance checks for Event summary (no failure
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
	"example.com/fixtures/features/event_summary"
	"example.com/fixtures/features/event_summary/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Organizer 7 owns event 1 (two sections); organizer 8 owns event 2.
func newConn(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO events (id, organizer_id, created_as, title, starts_at) VALUES (1, 7, 'organizer', 'a', 1), (2, 8, 'organizer', 'b', 2);
		INSERT INTO sections (event_id, name, capacity) VALUES (1, 'Floor', 100), (1, 'Balcony', 40), (2, 'Floor', 60)`); err != nil {
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
	mux.Handle(event_summary.Route, httpx.Bind(event_summary.Roles, event_summary.New(db.New(txn.DB(conn))).Handle))
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, req)
	return rec
}

// "Who may call it: anyone, signed in or not." The section count is public
// ("this read is not limited to sections of events you own"); `yours` is 1
// only for the event's organizer ("only events you own ... are read"), 0
// signed out (user 0), and the role is the empty text signed out.
func TestHTTPAnyoneMayCall(t *testing.T) {
	conn := newConn(t)
	for _, tc := range []struct {
		target, user, role string
		want               event_summary.Output
	}{
		{"/events/1/summary", "", "", event_summary.Output{EventID: 1, Sections: 2, Yours: 0, ViewerRole: ""}},
		{"/events/1/summary", "7", "organizer", event_summary.Output{EventID: 1, Sections: 2, Yours: 1, ViewerRole: "organizer"}},
		{"/events/1/summary", "8", "organizer", event_summary.Output{EventID: 1, Sections: 2, Yours: 0, ViewerRole: "organizer"}},
		{"/events/2/summary", "8", "customer", event_summary.Output{EventID: 2, Sections: 1, Yours: 1, ViewerRole: "customer"}},
		{"/events/9/summary", "7", "finance", event_summary.Output{EventID: 9, Sections: 0, Yours: 0, ViewerRole: "finance"}},
		{"/events/1/summary", "7", "superuser", event_summary.Output{EventID: 1, Sections: 2, Yours: 0, ViewerRole: ""}}, // undeclared role: not signed in
	} {
		rec := serve(t, conn, tc.target, tc.user, tc.role)
		var out event_summary.Output
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out != tc.want {
			t.Errorf("%s user %q role %q: status %d %s; want %+v", tc.target, tc.user, tc.role, rec.Code, rec.Body, tc.want)
		}
	}
}

// The user and role never come from the query string (400, also signed
// out), and no other query parameter is taken either (T4).
func TestHTTPQueryParametersAre400(t *testing.T) {
	conn := newConn(t)
	for _, target := range []string{"/events/1/summary?user=7", "/events/1/summary?role=admin", "/events/1/summary?USER=7", "/events/1/summary?x=1", "/events/1/summary?id=2"} {
		for _, user := range []string{"", "8"} {
			rec := serve(t, conn, target, user, "customer")
			var body httpx.ErrorBody
			if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != httpx.BadInput.Status || body.Error.ID != httpx.BadInput.ID {
				t.Errorf("%s (user %q): status %d %s", target, user, rec.Code, rec.Body)
			}
		}
	}
}
