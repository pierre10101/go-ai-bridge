// Package checks holds the acceptance checks for List customer invoices.
package checks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "example.com/fixtures"
	"example.com/fixtures/features/list_customer_invoices"
	"example.com/fixtures/features/list_customer_invoices/db"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
	"github.com/pierre10101/go-ai-bridge/runtime/store"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func newAction(t *testing.T) (*list_customer_invoices.Action, *sql.DB) {
	t.Helper()
	conn := openDB(t)
	return list_customer_invoices.New(db.New(txn.DB(conn))), conn
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := store.Open(context.Background(), ":memory:", app.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Acme')`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func handle(a *list_customer_invoices.Action, in list_customer_invoices.Input) (list_customer_invoices.Output, error) {
	return txn.Read(context.Background(), func(ctx context.Context) (list_customer_invoices.Output, error) {
		return a.Handle(ctx, in)
	})
}

func seedInvoices(t *testing.T, conn *sql.DB, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		_, err := conn.Exec(
			`INSERT INTO invoices (seq, customer_id, amount_cents, currency) VALUES (?, 1, ?, 'ZAR')`,
			i, int64(100*i),
		)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestListsNewestFirstWithLimit(t *testing.T) {
	a, conn := newAction(t)
	seedInvoices(t, conn, 5)
	out, err := handle(a, list_customer_invoices.Input{
		CustomerID: 1, After: page.StartCursor, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Invoices) != 2 {
		t.Fatalf("got %d invoices: %+v", len(out.Invoices), out.Invoices)
	}
	if string(out.Invoices[0].InvoiceNumber) != "INV-000005" || string(out.Invoices[1].InvoiceNumber) != "INV-000004" {
		t.Fatalf("order: %+v", out.Invoices)
	}
}

func TestNextPageAfterCursor(t *testing.T) {
	a, conn := newAction(t)
	seedInvoices(t, conn, 5)
	out, err := handle(a, list_customer_invoices.Input{CustomerID: 1, After: 4, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Invoices) != 2 || string(out.Invoices[0].InvoiceNumber) != "INV-000003" {
		t.Fatalf("got %+v", out.Invoices)
	}
}

func TestEmptyListForCustomerWithNoInvoices(t *testing.T) {
	a, _ := newAction(t)
	out, err := handle(a, list_customer_invoices.Input{
		CustomerID: 1, After: page.StartCursor, Limit: 20,
	})
	if err != nil || out.Invoices == nil || len(out.Invoices) != 0 {
		t.Fatalf("want empty list, got %+v err=%v", out, err)
	}
}

func TestF1_UnknownCustomerIsRefused(t *testing.T) {
	a, _ := newAction(t)
	_, err := handle(a, list_customer_invoices.Input{
		CustomerID: 99, After: page.StartCursor, Limit: 20,
	})
	if !errors.Is(err, list_customer_invoices.F1) {
		t.Fatalf("want F1, got %v", err)
	}
}

func TestF2_BadPageLimitIsRefused(t *testing.T) {
	a, _ := newAction(t)
	for _, lim := range []int64{0, -1, page.MaxPageSize + 1} {
		_, err := handle(a, list_customer_invoices.Input{
			CustomerID: 1, After: page.StartCursor, Limit: lim,
		})
		if !errors.Is(err, list_customer_invoices.F2) {
			t.Fatalf("limit %d: want F2, got %v", lim, err)
		}
	}
}

func TestF3_NonPositiveCursorIsRefused(t *testing.T) {
	a, _ := newAction(t)
	for _, after := range []int64{0, -1} {
		_, err := handle(a, list_customer_invoices.Input{
			CustomerID: 1, After: after, Limit: 20,
		})
		if !errors.Is(err, list_customer_invoices.F3) {
			t.Fatalf("after %d: want F3, got %v", after, err)
		}
	}
}

// Step 7: next_after is the last listed seq when the page is full, else 0;
// following it walks every invoice exactly once and stops when next_after is 0.
// Sending that 0 back as after is F3 (not a restart of the list).
func TestNextAfterWalksEveryPageOnce(t *testing.T) {
	a, conn := newAction(t)
	seedInvoices(t, conn, 5)
	var seen []string
	after, pages := page.StartCursor, 0
	for {
		out, err := handle(a, list_customer_invoices.Input{CustomerID: 1, After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, inv := range out.Invoices {
			seen = append(seen, string(inv.InvoiceNumber))
		}
		if out.NextAfter == 0 {
			_, err := handle(a, list_customer_invoices.Input{CustomerID: 1, After: 0, Limit: 2})
			if !errors.Is(err, list_customer_invoices.F3) {
				t.Fatalf("after=0 after last page: want F3, got %v", err)
			}
			break
		}
		if pages > 5 {
			t.Fatal("next_after never reached 0")
		}
		after = out.NextAfter
	}
	want := []string{"INV-000005", "INV-000004", "INV-000003", "INV-000002", "INV-000001"}
	if len(seen) != len(want) || pages != 3 {
		t.Fatalf("pages %d, saw %v", pages, seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("saw %v, want %v", seen, want)
		}
	}
}

func TestNextAfterOnFullAndShortPages(t *testing.T) {
	a, conn := newAction(t)
	seedInvoices(t, conn, 4)
	full, err := handle(a, list_customer_invoices.Input{CustomerID: 1, After: page.StartCursor, Limit: 2})
	if err != nil || full.NextAfter != 3 {
		t.Fatalf("full page: next_after %d, err %v (want 3, the last listed seq)", full.NextAfter, err)
	}
	short, err := handle(a, list_customer_invoices.Input{CustomerID: 1, After: page.StartCursor, Limit: 10})
	if err != nil || short.NextAfter != 0 {
		t.Fatalf("short page: next_after %d, err %v (want 0)", short.NextAfter, err)
	}
}
