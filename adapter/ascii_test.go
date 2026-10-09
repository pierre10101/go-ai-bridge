package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonASCIIInSQLIsRefused(t *testing.T) {
	dir := t.TempDir()
	queries := filepath.Join(dir, "queries")
	if err := os.MkdirAll(queries, 0o755); err != nil {
		t.Fatal(err)
	}
	// Em dash in a comment: the class of bug sqlc misreports far from the site.
	bad := "-- name: Q :one\n-- note: it does not write — loans\nSELECT COUNT(*) FROM books WHERE id = ?;\n"
	if err := os.WriteFile(filepath.Join(queries, "q.sql"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, err := checkASCII(dir, queries)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a non-ASCII refusal")
	}
	got := errs.Error()
	if !strings.Contains(got, "non-ASCII") || !strings.Contains(got, "U+2014") {
		t.Fatalf("refusal should name the construct and codepoint, got %s", got)
	}
}

func TestASCIIOnlySQLIsAccepted(t *testing.T) {
	dir := t.TempDir()
	queries := filepath.Join(dir, "queries")
	if err := os.MkdirAll(queries, 0o755); err != nil {
		t.Fatal(err)
	}
	ok := "-- name: Q :one\n-- note: it does not write - loans\nSELECT COUNT(*) FROM books WHERE id = ?;\n"
	if err := os.WriteFile(filepath.Join(queries, "q.sql"), []byte(ok), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte("CREATE TABLE books (id INTEGER PRIMARY KEY);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, err := checkASCII(dir, queries)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("ASCII-only SQL refused: %v", errs)
	}
}
