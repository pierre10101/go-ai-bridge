// Package txn gives each action call one database transaction.
//
// Every slice builds its sqlc queries on txn.DB(conn). Bind runs Handle
// through txn.Run, which puts an empty transaction slot in the context. The
// first statement a query sends (the grammar passes ctx to every query, S3)
// takes a connection and runs BEGIN IMMEDIATE on it; every later statement of
// the same call runs on that connection, inside that transaction. Run commits
// when Handle returns no error and rolls back when it returns any error (F-ID
// failures included), panics (failed assertions) or the commit fails.
//
// BEGIN IMMEDIATE takes SQLite's write lock at the first statement: from the
// action's first query until the transaction ends, no other connection can
// write (other writers wait up to busy_timeout, then fail). SQLite is the
// only driver the runtime supports; another database needs its own begin statement here.
//
// Read is the same, for actions that only read (GET): its transaction begins
// with plain BEGIN, so it takes no write lock, and runs with PRAGMA query_only
// on, so any write inside it fails. It still reads one consistent snapshot.
//
// This is plumbing outside features/, so generics, defer and recover are fine.
package txn

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// DBTX is what sqlc-generated code needs (the same method set as each slice's db.DBTX).
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type slotKey struct{}

// slot is one Run call's transaction; conn stays nil until the first statement.
type slot struct {
	db       *sql.DB
	conn     *sql.Conn
	readOnly bool
}

// beginFailed carries a begin error out of QueryRowContext, whose *sql.Row
// cannot hold one; Run recovers it and returns the error.
type beginFailed struct{ err error }

// DB returns the DBTX a slice's queries are built on: db.New(txn.DB(conn)).
// Its statements run only inside Run; outside Run they fail an assertion.
func DB(db *sql.DB) DBTX { return lazy{db} }

type lazy struct{ db *sql.DB }

func (l lazy) conn(ctx context.Context) (*sql.Conn, error) {
	s, _ := ctx.Value(slotKey{}).(*slot)
	assert.Pre(s != nil, "queries run inside txn.Run (one transaction per action call)")
	if s.conn == nil {
		c, err := l.db.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin transaction: %w", err)
		}
		begin := []string{"BEGIN IMMEDIATE"}
		if s.readOnly {
			begin = []string{"PRAGMA query_only = ON", "BEGIN"}
		}
		for _, stmt := range begin {
			if _, err := c.ExecContext(ctx, stmt); err != nil {
				if s.readOnly {
					_, _ = c.ExecContext(context.WithoutCancel(ctx), "PRAGMA query_only = OFF")
				}
				c.Close()
				return nil, fmt.Errorf("begin transaction: %w", err)
			}
		}
		s.db, s.conn = l.db, c
	}
	assert.Pre(s.db == l.db, "one action call uses one database")
	return s.conn, nil
}

func (l lazy) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c, err := l.conn(ctx)
	if err != nil {
		return nil, err
	}
	return c.ExecContext(ctx, q, args...)
}

func (l lazy) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	c, err := l.conn(ctx)
	if err != nil {
		return nil, err
	}
	return c.PrepareContext(ctx, q)
}

func (l lazy) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c, err := l.conn(ctx)
	if err != nil {
		return nil, err
	}
	return c.QueryContext(ctx, q, args...)
}

func (l lazy) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c, err := l.conn(ctx)
	if err != nil {
		panic(beginFailed{err})
	}
	return c.QueryRowContext(ctx, q, args...)
}

// Run calls fn with a context whose queries share one transaction, begun by
// the first statement. It commits if fn returns a nil error and rolls back if
// fn returns an error or panics (the panic continues after the rollback). A
// failed commit is rolled back and returned as the error, with the zero T.
func Run[T any](ctx context.Context, fn func(context.Context) (T, error)) (out T, err error) {
	return run(ctx, &slot{}, fn)
}

// Read is Run for an action that only reads: no write lock is taken, and a
// write inside fn fails (PRAGMA query_only). The connection's query_only
// setting is restored before it goes back to the pool.
func Read[T any](ctx context.Context, fn func(context.Context) (T, error)) (out T, err error) {
	return run(ctx, &slot{readOnly: true}, fn)
}

func run[T any](ctx context.Context, s *slot, fn func(context.Context) (T, error)) (out T, err error) {
	end := context.WithoutCancel(ctx) // a cancelled request must still end its transaction
	done := false
	defer func() {
		v := recover()
		if b, ok := v.(beginFailed); ok {
			v, out, err = nil, *new(T), b.err
		}
		if s.conn != nil {
			if !done {
				_, _ = s.conn.ExecContext(end, "ROLLBACK")
			}
			if s.readOnly {
				_, _ = s.conn.ExecContext(end, "PRAGMA query_only = OFF")
			}
			s.conn.Close()
		}
		if v != nil {
			panic(v)
		}
	}()
	out, err = fn(context.WithValue(ctx, slotKey{}, s))
	if err != nil || s.conn == nil {
		return out, err
	}
	if _, cerr := s.conn.ExecContext(end, "COMMIT"); cerr != nil {
		return *new(T), fmt.Errorf("commit: %w", cerr) // the deferred ROLLBACK ends it
	}
	done = true
	return out, nil
}
