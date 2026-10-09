// Package checks holds the acceptance checks for Release example.
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
	"example.com/fixtures/features/release_example"
	"example.com/fixtures/features/release_example/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

const t0 = 1_800_000_000

// newAction opens a database with seat 1 held by session 7 and seat 2 free.
func newAction(t *testing.T) (*release_example.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO seats (id, held_by, expires_at) VALUES (1, 7, ?), (2, 0, 0)`, t0+600); err != nil {
		t.Fatal(err)
	}
	return release_example.New(db.New(txn.DB(conn))), conn
}

// release calls the action the way httpx.Bind does: in one transaction.
func release(a *release_example.Action, seat, session int64) (release_example.Output, error) {
	in := release_example.Input{SeatID: seat, Session: session}
	return txn.Run(context.Background(), func(ctx context.Context) (release_example.Output, error) { return a.Handle(ctx, in) })
}

func holder(t *testing.T, conn *sql.DB, seat int64) (by, until int64) {
	t.Helper()
	if err := conn.QueryRow(`SELECT held_by, expires_at FROM seats WHERE id = ?`, seat).Scan(&by, &until); err != nil {
		t.Fatal(err)
	}
	return by, until
}

func TestReleasesOwnHold(t *testing.T) {
	a, conn := newAction(t)
	out, err := release(a, 1, 7)
	if err != nil || out != (release_example.Output{SeatID: 1}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if by, until := holder(t, conn, 1); by != 0 || until != 0 {
		t.Fatalf("seat still held by %d until %d", by, until)
	}
}

func TestF1_SessionRequired(t *testing.T) {
	a, conn := newAction(t)
	for _, s := range []int64{0, -1} {
		if _, err := release(a, 1, s); !errors.Is(err, release_example.F1) {
			t.Fatalf("session %d: want F1, got %v", s, err)
		}
	}
	if by, _ := holder(t, conn, 1); by != 7 {
		t.Fatalf("seat held by %d", by)
	}
}

// "Nothing was written in step 2": no seat, no change.
func TestF2_NoSuchSeat(t *testing.T) {
	a, _ := newAction(t)
	if _, err := release(a, 99, 7); !errors.Is(err, release_example.F2) {
		t.Fatalf("want F2, got %v", err)
	}
}

// Someone else's hold, or a free seat: F3, the hold stays as it was.
func TestF3_NotHeldByThisSession(t *testing.T) {
	a, conn := newAction(t)
	if _, err := release(a, 1, 8); !errors.Is(err, release_example.F3) {
		t.Fatalf("other session: want F3, got %v", err)
	}
	if by, until := holder(t, conn, 1); by != 7 || until != t0+600 {
		t.Fatalf("hold changed to %d until %d", by, until)
	}
	if _, err := release(a, 2, 7); !errors.Is(err, release_example.F3) {
		t.Fatalf("free seat: want F3, got %v", err)
	}
}

// Over HTTP the session comes from the cookie httpx.SessionCookie
// (httpx.SessionRule); a request that sends it is answered with 400 and
// changes nothing; without a valid cookie the session is 0, so F1.
func TestF1_HTTPSessionFromCookie(t *testing.T) {
	_, conn := newAction(t)
	mux := http.NewServeMux()
	mux.Handle(release_example.Route, httpx.Bind(release_example.Roles, release_example.New(db.New(txn.DB(conn))).Handle))
	post := func(cookie, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	for _, r := range []struct{ target, body string }{
		{"/holds/release", `{"seat_id": 1, "session": 7}`},
		{"/holds/release?session=7", `{"seat_id": 1}`},
	} {
		if rec := post("8", r.target, r.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bad_request") {
			t.Fatalf("caller-sent session %s %s: %d %s", r.target, r.body, rec.Code, rec.Body)
		}
	}
	for _, cookie := range []string{"", "0", "seven"} {
		if rec := post(cookie, "/holds/release", `{"seat_id": 1}`); rec.Code != release_example.F1.Status || !strings.Contains(rec.Body.String(), `"`+release_example.F1.ID+`"`) {
			t.Fatalf("cookie %q: %d %s", cookie, rec.Code, rec.Body)
		}
	}
	if by, _ := holder(t, conn, 1); by != 7 {
		t.Fatalf("seat held by %d after refused requests", by)
	}
	if rec := post("7", "/holds/release", `{"seat_id": 1}`); rec.Code != http.StatusCreated {
		t.Fatalf("release: %d %s", rec.Code, rec.Body)
	}
	if by, _ := holder(t, conn, 1); by != 0 {
		t.Fatalf("seat held by %d", by)
	}
}
