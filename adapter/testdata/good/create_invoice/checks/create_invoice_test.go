// Package checks holds the acceptance checks for Create invoice.
// Test names carry the F-IDs they cover: TestF<n>_... (CI checks coverage).
package checks

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/create_invoice"
	"example.com/fixtures/features/create_invoice/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func newAction(t *testing.T) (*create_invoice.Action, *sql.DB) {
	t.Helper()
	conn := openDB(t, ":memory:")
	return create_invoice.New(db.New(txn.DB(conn))), conn
}

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), path, app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Acme')`); err != nil {
		t.Fatal(err)
	}
	return conn
}

// handle calls the action the way httpx.Bind does: in one transaction (txn.Run).
func handle(a *create_invoice.Action, in create_invoice.Input) (create_invoice.Output, error) {
	return txn.Run(context.Background(), func(ctx context.Context) (create_invoice.Output, error) { return a.Handle(ctx, in) })
}

func countInvoices(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreatesSequentialInvoiceNumbers(t *testing.T) {
	a, _ := newAction(t)
	for i, want := range []string{"INV-000001", "INV-000002"} {
		out, err := handle(a, create_invoice.Input{CustomerID: 1, AmountCents: 1500, Currency: "ZAR"})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(out.InvoiceNumber) != want || out.Total.Cents != 1500 || out.Total.Currency != "ZAR" {
			t.Fatalf("call %d: got %+v", i, out)
		}
	}
}

func TestF1_UnknownCustomerIsRefused(t *testing.T) {
	a, conn := newAction(t)
	_, err := handle(a, create_invoice.Input{CustomerID: 99, AmountCents: 1500, Currency: "ZAR"})
	if !errors.Is(err, create_invoice.F1) {
		t.Fatalf("want F1, got %v", err)
	}
	if countInvoices(t, conn) != 0 {
		t.Fatal("F1 must not write")
	}
}

func TestF2_NonPositiveAmountIsRefused(t *testing.T) {
	a, conn := newAction(t)
	for _, cents := range []int64{0, -1} {
		_, err := handle(a, create_invoice.Input{CustomerID: 1, AmountCents: cents, Currency: "ZAR"})
		if !errors.Is(err, create_invoice.F2) {
			t.Fatalf("amount %d: want F2, got %v", cents, err)
		}
	}
	if countInvoices(t, conn) != 0 {
		t.Fatal("F2 must not write")
	}
}

func TestF3_UnsupportedCurrencyIsRefused(t *testing.T) {
	a, conn := newAction(t)
	_, err := handle(a, create_invoice.Input{CustomerID: 1, AmountCents: 1500, Currency: "GBP"})
	if !errors.Is(err, create_invoice.F3) {
		t.Fatalf("want F3, got %v", err)
	}
	if countInvoices(t, conn) != 0 {
		t.Fatal("F3 must not write")
	}
}

// TestRaceCustomerDeleteCannotSplitCheckAndInsert: steps 3-7 are one
// transaction holding the write lock from step 3. Another connection that
// tries to delete the customer between the check (step 3) and the insert
// (step 5) is locked out, so the action answers success, never a 500 for a
// customer that vanished mid-flight. (A delete that waits until the commit
// then fails the invoice's foreign key.)
func TestRaceCustomerDeleteCannotSplitCheckAndInsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	conn := openDB(t, path)
	other, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	var deleteErr error
	deletes := 0
	between := hook{DBTX: txn.DB(conn), before: func(query string) {
		if strings.Contains(query, "INSERT INTO invoices") { // after step 3, before step 5
			deletes++
			_, deleteErr = other.Exec(`DELETE FROM customers WHERE id = 1`)
		}
	}}
	a := create_invoice.New(db.New(between))
	out, err := handle(a, create_invoice.Input{CustomerID: 1, AmountCents: 1500, Currency: "ZAR"})
	if err != nil || out.InvoiceNumber != "INV-000001" {
		t.Fatalf("want success, got %+v, %v", out, err)
	}
	if deletes != 1 || deleteErr == nil || !strings.Contains(deleteErr.Error(), "locked") {
		t.Fatalf("the delete between step 3 and step 5 must be locked out; ran %d, got %v", deletes, deleteErr)
	}
	if _, err := other.Exec(`DELETE FROM customers WHERE id = 1`); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("after the commit, deleting the billed customer must fail its foreign key, got %v", err)
	}
	if countInvoices(t, conn) != 1 {
		t.Fatal("the invoice must be stored")
	}
}

// hook runs before each statement, to act between two steps of Handle.
type hook struct {
	db.DBTX
	before func(query string)
}

func (h hook) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	h.before(query)
	return h.DBTX.QueryRowContext(ctx, query, args...)
}

// TestAssertionAfterInsertWritesNothing: InvoiceNumberFor asserts the sequence
// fits six digits (step 6, after the insert in step 5). The 1,000,000th
// invoice fails it: the transaction is rolled back, so no invoice is stored.
func TestAssertionAfterInsertWritesNothing(t *testing.T) {
	a, conn := newAction(t)
	if _, err := conn.Exec(`INSERT INTO invoices (seq, customer_id, amount_cents, currency) VALUES (999999, 1, 100, 'ZAR')`); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if v, ok := recover().(assert.Violation); !ok || v.Message != "invoice sequence fits six digits" {
				t.Fatalf("want the step 6 assertion, got %v", v)
			}
		}()
		_, _ = handle(a, create_invoice.Input{CustomerID: 1, AmountCents: 1500, Currency: "ZAR"})
	}()
	if countInvoices(t, conn) != 1 {
		t.Fatalf("invoices %d: the insert from step 5 must be rolled back", countInvoices(t, conn))
	}
}

// TestInsertFailureWritesNothing: if the step 5 query fails (here a trigger
// deletes the customer inside the insert, so its foreign key fails), the
// answer is a plain error (HTTP 500 via httpx), not F1, and nothing is stored.
func TestInsertFailureWritesNothing(t *testing.T) {
	a, conn := newAction(t)
	if _, err := conn.Exec(`CREATE TRIGGER vanish BEFORE INSERT ON invoices
		BEGIN DELETE FROM customers WHERE id = NEW.customer_id; END`); err != nil {
		t.Fatal(err)
	}
	_, err := handle(a, create_invoice.Input{CustomerID: 1, AmountCents: 1500, Currency: "ZAR"})
	var f *failure.Failure
	if err == nil || errors.As(err, &f) {
		t.Fatalf("want a plain database error (HTTP 500 via httpx), got %v", err)
	}
	if countInvoices(t, conn) != 0 {
		t.Fatal("no invoice may be written")
	}
	var customers int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&customers); err != nil || customers != 1 {
		t.Fatalf("the trigger's delete must be rolled back too: customers %d (%v)", customers, err)
	}
}
