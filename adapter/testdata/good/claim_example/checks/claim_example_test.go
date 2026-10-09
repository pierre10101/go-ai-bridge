// Package checks holds the acceptance checks for Claim example.
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
	"time"

	app "example.com/fixtures"
	"example.com/fixtures/features/claim_example"
	"example.com/fixtures/features/claim_example/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

const t0 = 1_800_000_000 // any time: checks pass the current time in (T1)

func newAction(t *testing.T) (*claim_example.Action, *sql.DB) {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO seats (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	return claim_example.New(db.New(txn.DB(conn))), conn
}

// claim calls the action the way httpx.Bind does: in one transaction
// (txn.Run), with the session and the current time set by the server.
func claim(a *claim_example.Action, session, now int64) (claim_example.Output, error) {
	in := claim_example.Input{SeatID: 1, Session: session, Now: now}
	return txn.Run(context.Background(), func(ctx context.Context) (claim_example.Output, error) { return a.Handle(ctx, in) })
}

func holder(t *testing.T, conn *sql.DB) (by, until int64) {
	t.Helper()
	if err := conn.QueryRow(`SELECT held_by, expires_at FROM seats WHERE id = 1`).Scan(&by, &until); err != nil {
		t.Fatal(err)
	}
	return by, until
}

func TestClaimsAFreeSeat(t *testing.T) {
	a, conn := newAction(t)
	out, err := claim(a, 7, t0)
	if err != nil || out != (claim_example.Output{SeatID: 1, Now: t0}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if by, until := holder(t, conn); by != 7 || until != t0+600 {
		t.Fatalf("stored hold %d until %d", by, until)
	}
}

// The English: "`expires_at` is no later than the current time". A hold
// that expires one second or more after now blocks the seat (F1, nothing
// written), also for the session that holds it; one whose expires_at is
// exactly now has expired and does not.
func TestF1_HoldNotExpired(t *testing.T) {
	a, conn := newAction(t)
	if _, err := claim(a, 7, t0); err != nil {
		t.Fatal(err)
	}
	for _, later := range []int64{0, 1, 599} {
		for _, session := range []int64{8, 7} {
			if _, err := claim(a, session, t0+later); !errors.Is(err, claim_example.F1) {
				t.Fatalf("session %d, %d seconds later: want F1, got %v", session, later, err)
			}
			if by, until := holder(t, conn); by != 7 || until != t0+600 {
				t.Fatalf("session %d, %d seconds later: hold changed to %d until %d", session, later, by, until)
			}
		}
	}
	if _, err := claim(a, 8, t0+600); err != nil {
		t.Fatalf("at expires_at the hold has expired and no longer blocks: %v", err)
	}
	if by, until := holder(t, conn); by != 8 || until != t0+1200 {
		t.Fatalf("hold %d until %d, want 8 until %d", by, until, t0+1200)
	}
}

func TestF1_NoSuchSeat(t *testing.T) {
	a, _ := newAction(t)
	in := claim_example.Input{SeatID: 99, Session: 7, Now: t0}
	_, err := txn.Run(context.Background(), func(ctx context.Context) (claim_example.Output, error) { return a.Handle(ctx, in) })
	if !errors.Is(err, claim_example.F1) {
		t.Fatalf("want F1, got %v", err)
	}
}

func TestF2_SessionRequired(t *testing.T) {
	a, conn := newAction(t)
	for _, session := range []int64{0, -1} {
		if _, err := claim(a, session, t0); !errors.Is(err, claim_example.F2) {
			t.Fatalf("session %d: want F2, got %v", session, err)
		}
	}
	if by, _ := holder(t, conn); by != 0 {
		t.Fatalf("seat held by %d", by)
	}
}

// Over HTTP the server sets now (httpx.ClockRule) and the session from the
// cookie (httpx.SessionRule); the caller can send neither, so it cannot
// claim a seat in someone else's name.
func TestF2_HTTPServerSetsSessionAndTime(t *testing.T) {
	_, conn := newAction(t)
	old := httpx.Now
	httpx.Now = func() time.Time { return time.Unix(t0, 0) }
	t.Cleanup(func() { httpx.Now = old })
	mux := http.NewServeMux()
	mux.Handle(claim_example.Route, httpx.Bind(claim_example.Roles, claim_example.New(db.New(txn.DB(conn))).Handle))
	post := func(cookie, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/holds", strings.NewReader(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	for _, body := range []string{`{"seat_id": 1, "now": 1}`, `{"seat_id": 1, "session": 8}`, `{"seat_id": 1, "person_id": 8}`} {
		if rec := post("7", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("caller-sent %s: status %d", body, rec.Code)
		}
	}
	if rec := post("", `{"seat_id": 1}`); rec.Code != claim_example.F2.Status || !strings.Contains(rec.Body.String(), `"F2"`) {
		t.Fatalf("no cookie: status %d body %s", rec.Code, rec.Body)
	}
	if rec := post("7", `{"seat_id": 1}`); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"now":1800000000`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if by, until := holder(t, conn); by != 7 || until != t0+600 {
		t.Fatalf("stored hold %d until %d", by, until)
	}
	if rec := post("8", `{"seat_id": 1}`); rec.Code != http.StatusConflict {
		t.Fatalf("second claim: status %d body %s", rec.Code, rec.Body)
	}
}
