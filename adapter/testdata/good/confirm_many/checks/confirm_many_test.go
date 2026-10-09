// Package checks holds the acceptance checks for Confirm many.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
// Every check runs against real SQLite (runtime/store), with the queries sqlc
// generates for IN (sqlc.slice(ids)).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/confirm_many"
	"example.com/fixtures/features/confirm_many/db"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

const t0 = 1_800_000_000

type ticket struct {
	id        int64
	heldBy    string
	expiresAt int64
	soldTo    string
}

// seed: tickets 1-3 held by session s7 until t0+600, ticket 4 held by s8,
// ticket 5 free, ticket 6 already sold to s7.
var seed = []ticket{
	{1, "s7", t0 + 600, ""},
	{2, "s7", t0 + 600, ""},
	{3, "s7", t0 + 600, ""},
	{4, "s8", t0 + 600, ""},
	{5, "", 0, ""},
	{6, "", 0, "s7"},
}

func open(t *testing.T, path string, rows []ticket) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), path, app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for _, r := range rows {
		if _, err := conn.Exec(`INSERT INTO tickets (id, held_by, expires_at, sold_to) VALUES (?, ?, ?, ?)`, r.id, r.heldBy, r.expiresAt, r.soldTo); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}

func newAction(t *testing.T, rows []ticket) (*confirm_many.Action, *sql.DB) {
	t.Helper()
	conn := open(t, ":memory:", rows)
	return confirm_many.New(db.New(txn.DB(conn))), conn
}

// confirm calls the action the way httpx.Bind does: in one transaction.
func confirm(a *confirm_many.Action, session string, now int64, ids ...int64) (confirm_many.Output, error) {
	in := confirm_many.Input{TicketIDs: ids, Session: session, Now: now}
	return txn.Run(context.Background(), func(ctx context.Context) (confirm_many.Output, error) { return a.Handle(ctx, in) })
}

func tickets(t *testing.T, conn *sql.DB) []ticket {
	t.Helper()
	rows, err := conn.Query(`SELECT id, held_by, expires_at, sold_to FROM tickets ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []ticket
	for rows.Next() {
		var r ticket
		if err := rows.Scan(&r.id, &r.heldBy, &r.expiresAt, &r.soldTo); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// unchanged fails unless the table is exactly as seeded: nothing was written.
func unchanged(t *testing.T, conn *sql.DB, want []ticket) {
	t.Helper()
	got := tickets(t, conn)
	if len(got) != len(want) {
		t.Fatalf("tickets %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ticket %d is %+v, want %+v (something was written)", want[i].id, got[i], want[i])
		}
	}
}

func TestConfirmsAll(t *testing.T) {
	a, conn := newAction(t, seed)
	out, err := confirm(a, "s7", t0, 3, 1, 2)
	if err != nil || out != (confirm_many.Output{Confirmed: 3}) {
		t.Fatalf("out %+v err %v", out, err)
	}
	want := append([]ticket{}, seed...)
	for i := 0; i < 3; i++ {
		want[i] = ticket{want[i].id, "", t0 + 600, "s7"}
	}
	unchanged(t, conn, want)
}

func TestF1_SessionRequired(t *testing.T) {
	a, conn := newAction(t, seed)
	if _, err := confirm(a, "", t0, 1, 2, 3); !errors.Is(err, confirm_many.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	unchanged(t, conn, seed)
}

// One expired hold among three: nothing is sold, the two good tickets are
// rolled back. The boundary is exact: expires_at = now has expired,
// expires_at = now + 1 has not.
func TestF2_OneHoldExpired(t *testing.T) {
	rows := append([]ticket{}, seed...)
	rows[1].expiresAt = t0
	a, conn := newAction(t, rows)
	if _, err := confirm(a, "s7", t0, 1, 2, 3); !errors.Is(err, confirm_many.F2) {
		t.Fatalf("want F2, got %v", err)
	}
	unchanged(t, conn, rows)
	if _, err := confirm(a, "s7", t0-1, 1, 2, 3); err != nil {
		t.Fatalf("one second before it expires: %v", err)
	}
}

// A ticket this session does not hold: someone else's, a free one, one
// already sold, or no such ticket. Everything rolls back.
func TestF3_NotHeldByThisSession(t *testing.T) {
	for name, ids := range map[string][]int64{
		"someone else's": {1, 2, 4},
		"free":           {1, 5},
		"already sold":   {6, 1},
		"no such ticket": {1, 2, 3, 99},
	} {
		t.Run(name, func(t *testing.T) {
			a, conn := newAction(t, seed)
			if _, err := confirm(a, "s7", t0, ids...); !errors.Is(err, confirm_many.F3) {
				t.Fatalf("want F3, got %v", err)
			}
			unchanged(t, conn, seed)
		})
	}
}

// Handle called directly with a repeated id (httpx.Bind refuses that with
// 400, see http_test.go): the count cannot match, so F3 and nothing written.
func TestF3_RepeatedIDCalledDirectly(t *testing.T) {
	a, conn := newAction(t, seed)
	if _, err := confirm(a, "s7", t0, 1, 1); !errors.Is(err, confirm_many.F3) {
		t.Fatalf("want F3, got %v", err)
	}
	unchanged(t, conn, seed)
}

// Concurrency: separate connections to one database file confirm at the
// same time. The claim checks and writes in one statement inside a
// BEGIN IMMEDIATE transaction, so the same basket is sold exactly once
// (every other call gets F3 and writes nothing), and a disjoint basket of
// another session is not disturbed.
func TestF3_ConcurrentConfirmsSellOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.db")
	open(t, path, append(append([]ticket{}, seed...), ticket{7, "s8", t0 + 600, ""}))
	const n = 8
	actions := make([]*confirm_many.Action, n+1)
	for i := range actions {
		actions[i] = confirm_many.New(db.New(txn.DB(open(t, path, nil))))
	}
	var wg sync.WaitGroup
	errs := make([]error, n+1)
	for i := 0; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == n {
				_, errs[i] = confirm(actions[i], "s8", t0, 4, 7)
				return
			}
			_, errs[i] = confirm(actions[i], "s7", t0, 1, 2, 3)
		}(i)
	}
	wg.Wait()
	sold := 0
	for i, err := range errs[:n] {
		switch {
		case err == nil:
			sold++
		case !errors.Is(err, confirm_many.F3):
			t.Fatalf("call %d: want success or F3, got %v", i, err)
		}
	}
	if sold != 1 || errs[n] != nil {
		t.Fatalf("basket sold %d times; other session: %v", sold, errs[n])
	}
	for _, r := range tickets(t, open(t, path, nil)) {
		switch r.id {
		case 1, 2, 3, 6:
			if r.soldTo != "s7" || r.heldBy != "" {
				t.Fatalf("ticket %+v", r)
			}
		case 4, 7:
			if r.soldTo != "s8" || r.heldBy != "" {
				t.Fatalf("ticket %+v", r)
			}
		}
	}
}
