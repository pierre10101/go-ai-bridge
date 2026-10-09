// Package list_customer_invoices is the List customer invoices slice. The why
// lives in intent.md; the English review rendering lives in
// list_customer_invoices.en (generated, golden).
package list_customer_invoices

import (
	"context"
	"net/http"

	"example.com/fixtures/features/list_customer_invoices/db"
	"example.com/fixtures/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
)

// Route is the HTTP contract (Go 1.22 method + path pattern).
const Route = "GET /customers/{id}/invoices"

// Roles: only signed-in finance staff and admins may call it (A1); httpx.Bind
// answers anyone else with 401 or 403 before the action runs.
var Roles = httpx.Roles("finance", "admin")

// Input is what the caller sends (path + query on GET).
type Input struct {
	CustomerID int64 `json:"customer_id" path:"id"`
	After      int64 `json:"after" query:"after"`
	Limit      int64 `json:"limit" query:"limit"`
}

// Output is what the caller gets back: one page of invoices and the cursor
// of the next page (0 when this is the last page).
type Output struct {
	Invoices  []domain.InvoiceSummary `json:"invoices"`
	NextAfter int64                   `json:"next_after"`
}

// Failure cases. IDs match intent.md and checks/.
var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "customer does not exist")
	F2 = failure.New("F2", http.StatusBadRequest, "page limit is out of range")
	F3 = failure.New("F3", http.StatusBadRequest, "page cursor must not be negative")
)

// Action lists one page of a customer's invoices.
type Action struct {
	q *db.Queries
}

// New wires the action. Constructor injection only.
func New(q *db.Queries) *Action {
	return &Action{q: q}
}

// Handle is the contract: Input in, Output or a failure out.
//
// No preconditions: every value comes from the request (F1-F3) or the database.
// httpx.Bind runs a GET in one read-only transaction (no write lock).
// Bind maps a missing or zero `after` to page.StartCursor before Handle runs.
func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if !page.IsPageLimit(in.Limit) {
		return Output{}, F2
	}
	if in.After < 0 {
		return Output{}, F3
	}

	customers, err := a.q.CustomerExists(ctx, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	if customers == 0 {
		return Output{}, F1
	}

	rows, err := a.q.ListCustomerInvoices(ctx, db.ListCustomerInvoicesParams{
		CustomerID: in.CustomerID,
		After:      in.After,
		Limit:      in.Limit,
	})
	if err != nil {
		return Output{}, err
	}

	items := make([]domain.InvoiceSummary, len(rows))
	for i, row := range rows {
		items[i] = domain.InvoiceSummary{
			InvoiceNumber: domain.InvoiceNumberFor(row.Seq),
			Total:         domain.Money{Cents: row.AmountCents, Currency: row.Currency},
		}
	}

	next := page.NextAfter(rows, "seq", in.Limit)

	out := Output{Invoices: items, NextAfter: next}
	assert.Post(out.Invoices != nil, "an empty page is [], not null")
	return out, nil
}
