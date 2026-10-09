// Deliberate rule break: an AI "helpfully" added a retry loop around the insert.
// bridge-en must refuse this file. It is testdata, so `go build` ignores it.
package retry_loop

import (
	"context"
	"net/http"

	"example.com/fixtures/features/retry_loop/db"
	"example.com/fixtures/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /invoices"

type Input struct {
	CustomerID  int64  `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

type Output struct {
	InvoiceNumber domain.InvoiceNumber `json:"invoice_number"`
}

var (
	F2 = failure.New("F2", http.StatusUnprocessableEntity, "amount must be greater than zero")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action {
	return &Action{q: q}
}

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	assert.Pre(a.q != nil, "queries are wired")

	if in.AmountCents <= 0 {
		return Output{}, F2
	}

	params := db.InsertInvoiceParams{CustomerID: in.CustomerID, AmountCents: in.AmountCents, Currency: in.Currency}
	for attempt := 0; attempt < 3; attempt++ {
		row, err := a.q.InsertInvoice(ctx, params)
		if err == nil {
			return Output{InvoiceNumber: domain.InvoiceNumberFor(row.Seq)}, nil
		}
	}
	return Output{}, nil
}

// Roles: anyone may call it (A1); this fixture is about another rule.
var Roles = httpx.Public
