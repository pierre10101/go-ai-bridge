package store

import (
	"context"
	"testing"
)

func TestOpenAppliesSchemaAndForeignKeys(t *testing.T) {
	db, err := Open(context.Background(), ":memory:", `
CREATE TABLE parents (id INTEGER PRIMARY KEY);
CREATE TABLE children (parent_id INTEGER NOT NULL REFERENCES parents (id));`)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO children (parent_id) VALUES (1)`); err == nil {
		t.Fatal("foreign keys are not enforced")
	}
	if _, err := Open(context.Background(), ":memory:", "NOT SQL"); err == nil {
		t.Fatal("a broken schema must fail Open")
	}
}
