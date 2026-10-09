package main

import (
	"database/sql"
	"net/http"

	"example.com/fixtures/features/claim_example"
	claimdb "example.com/fixtures/features/claim_example/db"
	"example.com/fixtures/features/create_invoice"
	createdb "example.com/fixtures/features/create_invoice/db"
	"example.com/fixtures/features/list_customer_invoices"
	listdb "example.com/fixtures/features/list_customer_invoices/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// Routes wires every slice. One line per feature: Route constant -> Handle.
// Queries go through txn.DB, so httpx.Bind runs each call in one transaction.
func Routes(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(claim_example.Route, httpx.Bind(claim_example.New(claimdb.New(txn.DB(db))).Handle))
	mux.Handle(create_invoice.Route, httpx.Bind(create_invoice.New(createdb.New(txn.DB(db))).Handle))
	mux.Handle(list_customer_invoices.Route, httpx.Bind(list_customer_invoices.New(listdb.New(txn.DB(db))).Handle))
	return mux
}
