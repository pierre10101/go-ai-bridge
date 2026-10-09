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

// claim calls the action the way httpx.Bind does: in one transaction (txn.Run).
func claim(a *claim_example.Action, person, now int64) (claim_example.Output, error) {
	in := claim_example.Input{SeatID: 1, PersonID: person, Now: now}
	return txn.Run(context.Background(), func(ctx context.Context) (claim_example.Output, error) { return a.Handle(ctx, in) })
}

func holder(t *testing.T, conn *sql.DB) (by, at int64) {
	t.Helper()
	if err := conn.QueryRow(`SELECT held_by, held_at FROM seats WHERE id = 1`).Scan(&by, &at); err != nil {
		t.Fatal(err)
	}
	return by, at
}

func TestClaimsAFreeSeat(t *testing.T) {
	a, conn := newAction(t)
	out, err := claim(a, 7, t0)
	if err != nil || out != (claim_example.Output{SeatID: 1, HeldBy: 7, HeldAt: t0}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	if by, at := holder(t, conn); by != 7 || at != t0 {
		t.Fatalf("stored hold %d at %d", by, at)
	}
}

// The English: "`held_at` is 10 minutes or more before the current time".
// A hold taken 599 seconds ago blocks the seat (F1, nothing written); one
// taken exactly 600 seconds ago does not.
func TestF1_SeatHeldLessThanTenMinutesAgo(t *testing.T) {
	a, conn := newAction(t)
	if _, err := claim(a, 7, t0); err != nil {
		t.Fatal(err)
	}
	for _, later := range []int64{0, 1, 599} {
		if _, err := claim(a, 8, t0+later); !errors.Is(err, claim_example.F1) {
			t.Fatalf("%d seconds later: want F1, got %v", later, err)
		}
		if by, at := holder(t, conn); by != 7 || at != t0 {
			t.Fatalf("%d seconds later: hold changed to %d at %d", later, by, at)
		}
	}
	if _, err := claim(a, 8, t0+600); err != nil {
		t.Fatalf("exactly 10 minutes later the hold no longer blocks: %v", err)
	}
	if by, at := holder(t, conn); by != 8 || at != t0+600 {
		t.Fatalf("hold %d at %d, want 8 at %d", by, at, t0+600)
	}
}

func TestF1_NoSuchSeat(t *testing.T) {
	a, _ := newAction(t)
	in := claim_example.Input{SeatID: 99, PersonID: 7, Now: t0}
	_, err := txn.Run(context.Background(), func(ctx context.Context) (claim_example.Output, error) { return a.Handle(ctx, in) })
	if !errors.Is(err, claim_example.F1) {
		t.Fatalf("want F1, got %v", err)
	}
}

func TestF2_PersonRequired(t *testing.T) {
	a, conn := newAction(t)
	for _, person := range []int64{0, -1} {
		if _, err := claim(a, person, t0); !errors.Is(err, claim_example.F2) {
			t.Fatalf("person %d: want F2, got %v", person, err)
		}
	}
	if by, _ := holder(t, conn); by != 0 {
		t.Fatalf("seat held by %d", by)
	}
}

// Over HTTP the server sets now (httpx.ClockRule); the caller cannot.
func TestHTTPClaimUsesServerTime(t *testing.T) {
	_, conn := newAction(t)
	old := httpx.Now
	httpx.Now = func() time.Time { return time.Unix(t0, 0) }
	t.Cleanup(func() { httpx.Now = old })
	mux := http.NewServeMux()
	mux.Handle(claim_example.Route, httpx.Bind(claim_example.New(db.New(txn.DB(conn))).Handle))
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/holds", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"seat_id": 1, "person_id": 7, "now": 1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("caller-sent now: status %d", rec.Code)
	}
	if rec := post(`{"seat_id": 1, "person_id": 7}`); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"held_at":1800000000`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if rec := post(`{"seat_id": 1, "person_id": 8}`); rec.Code != http.StatusConflict {
		t.Fatalf("second claim: status %d body %s", rec.Code, rec.Body)
	}
}
