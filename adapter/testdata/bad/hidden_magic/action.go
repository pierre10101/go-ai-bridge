// Deliberate rule break: string formatting, a private helper, arithmetic and a
// closure inside an action. bridge-en must refuse each with its location.
package hidden_magic

import (
	"context"
	"fmt"

	"example.com/fixtures/features/hidden_magic/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

const Route = "POST /invoices"

type Input struct {
	AmountCents int64   `json:"amount_cents"`
	Lines       []int64 `json:"lines"`
}

type Output struct {
	Label string `json:"label"`
}

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action {
	return &Action{q: q}
}

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	assert.Pre(a.q != nil, "queries are wired")
	total := in.AmountCents * 115 / 100
	label := func() string { return fmt.Sprint(total) }
	out := Output{Label: label()}
	assert.Post(out.Label != "", "label is set")
	return out, nil
}

func vat(cents int64) int64 { return cents * 15 / 100 }
