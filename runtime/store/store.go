// Package store opens the SQLite database (pure Go driver) and applies the
// app's schema.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Open opens (or creates) the database at path and applies schema, the app's
// plain-SQL schema (the same file sqlc reads; embed it with go:embed). Use
// ":memory:" for tests.
// Actions never use the *sql.DB directly: their queries go through
// txn.DB(db), one transaction per call (see runtime/txn).
func Open(ctx context.Context, path, schema string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite has one writer; also keeps :memory: on one connection.
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}
