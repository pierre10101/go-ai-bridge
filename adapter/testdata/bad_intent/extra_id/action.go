// Package create_invoice is the Create invoice slice. The why lives in intent.md;
// the English review rendering lives in create_invoice.en (generated, golden).
package create_invoice

import (
	"context"
	"net/http"

	"example.com/fixtures/features/create_invoice/db"
	"example.com/fixtures/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

// Route is the HTTP contract (Go 1.22 method + path pattern).
const Route = "POST /invoices"

// Input is what the caller sends.
type Input struct {
	CustomerID  int64  `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// Output is what the caller gets back.
type Output struct {
	InvoiceNumber domain.InvoiceNumber `json:"invoice_number"`
	CustomerID    int64                `json:"customer_id"`
	Total         domain.Money         `json:"total"`
}

// Failure cases. IDs match intent.md and checks/.
var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "customer does not exist")
	F2 = failure.New("F2", http.StatusUnprocessableEntity, "amount must be greater than zero")
	F3 = failure.New("F3", http.StatusUnprocessableEntity, "currency is not supported")
)

// Action creates one invoice.
type Action struct {
	q *db.Queries
}

// New wires the action. Constructor injection only.
func New(q *db.Queries) *Action {
	return &Action{q: q}
}

// Handle is the contract: Input in, Output or a failure out.
//
// No preconditions: every value Handle reads comes from the request (checked
// by F1-F3) or the database. The old "queries are wired" / "context is set"
// asserts were fillers (net/http never passes a nil context; New is the only
// way to build an Action), so they were removed rather than rendered as if
// they were guarantees. See RULEBOOK.md, S1 "meaningful precondition".
func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.AmountCents <= 0 {
		return Output{}, F2
	}
	if !domain.IsSupportedCurrency(in.Currency) {
		return Output{}, F3
	}

	customers, err := a.q.CustomerExists(ctx, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	if customers == 0 {
		return Output{}, F1
	}

	row, err := a.q.InsertInvoice(ctx, db.InsertInvoiceParams{
		CustomerID:  in.CustomerID,
		AmountCents: in.AmountCents,
		Currency:    in.Currency,
	})
	if err != nil {
		return Output{}, err
	}

	out := Output{
		InvoiceNumber: domain.InvoiceNumberFor(row.Seq),
		CustomerID:    row.CustomerID,
		Total:         domain.Money{Cents: row.AmountCents, Currency: row.Currency},
	}
	assert.Post(domain.IsValidInvoiceNumber(out.InvoiceNumber), "invoice number is well formed")
	assert.Post(out.CustomerID == in.CustomerID, "invoice belongs to the requested customer")
	assert.Post(out.Total.Cents == in.AmountCents, "stored amount equals requested amount")
	return out, nil
}

// Roles: anyone may call it (A1); this fixture is about another rule.
var Roles = httpx.Public
