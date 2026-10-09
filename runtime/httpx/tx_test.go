package httpx

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// These tests prove TxRule: Bind runs Handle in one transaction that begins at
// the first query with the write lock, is committed only when Handle succeeds
// (before the answer), and is rolled back when Handle stops in any way.

func txDB(t *testing.T) (*sql.DB, *sql.DB, txn.DBTX) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tx.db")
	// The same settings as store.Open, with a table of its own so
	// these tests do not depend on the app's schema.
	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// other is a second connection, as another process would have, that never waits for locks.
	other, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	return conn, other, txn.DB(conn)
}

func customers(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func addCustomer(ctx context.Context, q txn.DBTX) error {
	_, err := q.ExecContext(ctx, `INSERT INTO customers (id, name) VALUES (7, 'x')`)
	return err
}

// "It is committed in step {commit}": a successful Handle's writes are stored.
func TestTxCommitsOnSuccess(t *testing.T) {
	conn, _, q := txDB(t)
	rec, _, _ := serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) {
		return out{OK: true}, addCustomer(ctx, q)
	})
	if rec.Code != http.StatusCreated || customers(t, conn) != 1 {
		t.Fatalf("status %d customers %d", rec.Code, customers(t, conn))
	}
}

// "If any of steps {first} to {last} stops the action, the transaction is
// rolled back and nothing is written": F-ID, query error and failed assertion.
func TestTxRollsBackWhenHandleStops(t *testing.T) {
	stops := map[string]func() (out, error){
		"F-ID failure": func() (out, error) { return out{}, errBoom },
		"error":        func() (out, error) { return out{}, errors.New("query failed") },
		"assertion":    func() (out, error) { assert.Post(false, "broken"); return out{}, nil },
	}
	for name, stop := range stops {
		t.Run(name, func(t *testing.T) {
			conn, _, q := txDB(t)
			rec, _, _ := serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) {
				if err := addCustomer(ctx, q); err != nil {
					t.Fatal(err)
				}
				return stop()
			})
			if rec.Code < 400 || customers(t, conn) != 0 {
				t.Fatalf("status %d customers %d: the write must be rolled back", rec.Code, customers(t, conn))
			}
		})
	}
}

// A failed commit answers Internal and writes nothing. A deferred foreign key
// makes COMMIT itself fail; the connection must come back usable.
func TestTxCommitFailureIsInternalAndWritesNothing(t *testing.T) {
	conn, _, q := txDB(t)
	if _, err := conn.Exec(`CREATE TABLE notes (customer_id INTEGER REFERENCES customers (id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
		t.Fatal(err)
	}
	rec, eb, logs := serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) {
		if err := addCustomer(ctx, q); err != nil {
			t.Fatal(err)
		}
		_, err := q.ExecContext(ctx, `INSERT INTO notes (customer_id) VALUES (999)`)
		return out{OK: true}, err
	})
	if rec.Code != Internal.Status || eb.Error.Message != Internal.Message || !strings.Contains(logs, "commit") {
		t.Fatalf("status %d body %s logs %q", rec.Code, rec.Body, logs)
	}
	if customers(t, conn) != 0 {
		t.Fatal("a failed commit must write nothing")
	}
	rec, _, _ = serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) { return out{OK: true}, addCustomer(ctx, q) })
	if rec.Code != http.StatusCreated || customers(t, conn) != 1 {
		t.Fatalf("connection left in a transaction after a failed commit: status %d", rec.Code)
	}
}

// "It begins with the query in step {first} and holds the database's write
// lock until it ends": after the first query (even a read), another
// connection cannot write; before any query, it can.
func TestTxWriteLockFromFirstQuery(t *testing.T) {
	conn, other, q := txDB(t)
	var before, after, claim error
	rec, _, _ := serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) {
		_, before = other.Exec(`INSERT INTO customers (id, name) VALUES (1, 'before')`)
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&n); err != nil {
			return out{}, err
		}
		_, after = other.Exec(`INSERT INTO customers (id, name) VALUES (2, 'during')`)
		// Claiming the write lock must fail too (it would succeed under a
		// deferred BEGIN, and then this transaction's own write would fail).
		c, err := other.Conn(ctx)
		if err != nil {
			return out{}, err
		}
		defer c.Close()
		if _, claim = c.ExecContext(ctx, "BEGIN IMMEDIATE"); claim == nil {
			_, _ = c.ExecContext(ctx, "ROLLBACK")
		}
		return out{OK: true}, addCustomer(ctx, q)
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if before != nil {
		t.Fatalf("no transaction before the first query, but another writer failed: %v", before)
	}
	if after == nil || !strings.Contains(after.Error(), "locked") {
		t.Fatalf("another writer must be locked out after the first query, got %v", after)
	}
	if claim == nil || !strings.Contains(claim.Error(), "locked") {
		t.Fatalf("another connection must not get the write lock after the first query, got %v", claim)
	}
	if customers(t, conn) != 2 {
		t.Fatalf("customers %d, want the one written before the transaction and the one written in it", customers(t, conn))
	}
}

// Outside Run (Bind), queries fail an assertion instead of running untransacted.
func TestTxQueriesNeedRun(t *testing.T) {
	_, _, q := txDB(t)
	defer func() {
		if _, ok := recover().(assert.Violation); !ok {
			t.Fatal("want an assertion failure")
		}
	}()
	_ = addCustomer(context.Background(), q)
}

// These prove ReadTxRule: a GET runs in a read-only transaction that takes no
// write lock, reads one snapshot, and in which a write fails.

// "takes no write lock": while a GET is inside its transaction, another
// connection can still take the write lock (it could not during a POST).
func TestReadTxTakesNoWriteLock(t *testing.T) {
	conn, other, q := txDB(t)
	if _, err := conn.Exec(`INSERT INTO customers (id, name) VALUES (1, 'a')`); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := serve(t, http.MethodGet, full, func(ctx context.Context, _ in) (out, error) {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&n); err != nil {
			return out{}, err
		}
		tx, err := other.Begin()
		if err != nil {
			return out{}, err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`INSERT INTO customers (id, name) VALUES (2, 'b')`); err != nil {
			t.Errorf("another connection could not write during a GET: %v", err)
		}
		return out{OK: n == 1}, nil
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

// "a write inside it fails", and the connection is writable again afterwards.
func TestReadTxRefusesWrites(t *testing.T) {
	conn, _, q := txDB(t)
	rec, _, _ := serve(t, http.MethodGet, full, func(ctx context.Context, _ in) (out, error) {
		return out{OK: true}, addCustomer(ctx, q)
	})
	if rec.Code != http.StatusInternalServerError || customers(t, conn) != 0 {
		t.Fatalf("status %d customers %d", rec.Code, customers(t, conn))
	}
	rec, _, _ = serve(t, http.MethodPost, full, func(ctx context.Context, _ in) (out, error) {
		return out{OK: true}, addCustomer(ctx, q)
	})
	if rec.Code != http.StatusCreated || customers(t, conn) != 1 {
		t.Fatalf("after a GET the connection stayed read-only: status %d customers %d", rec.Code, customers(t, conn))
	}
}

// "every query in it reads the same snapshot": a write committed by another
// connection between two reads of one GET is not seen by the second read.
func TestReadTxReadsOneSnapshot(t *testing.T) {
	conn, other, q := txDB(t)
	if _, err := conn.Exec(`INSERT INTO customers (id, name) VALUES (1, 'a')`); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := serve(t, http.MethodGet, full, func(ctx context.Context, _ in) (out, error) {
		var first, second int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&first); err != nil {
			return out{}, err
		}
		_, werr := other.Exec(`INSERT INTO customers (id, name) VALUES (2, 'b')`)
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&second); err != nil {
			return out{}, err
		}
		if second != first {
			t.Errorf("second read saw another connection's write (first %d, second %d, write err %v)", first, second, werr)
		}
		return out{OK: true}, nil
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}
