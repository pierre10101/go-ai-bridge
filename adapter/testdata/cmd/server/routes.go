package main

import (
	"database/sql"
	"net/http"

	"example.com/fixtures/features/add_section"
	addsectiondb "example.com/fixtures/features/add_section/db"
	"example.com/fixtures/features/admin_rename_event"
	adminrenamedb "example.com/fixtures/features/admin_rename_event/db"
	"example.com/fixtures/features/admin_rename_section"
	adminsectiondb "example.com/fixtures/features/admin_rename_section/db"
	"example.com/fixtures/features/claim_example"
	claimdb "example.com/fixtures/features/claim_example/db"
	"example.com/fixtures/features/confirm_many"
	confirmdb "example.com/fixtures/features/confirm_many/db"
	"example.com/fixtures/features/create_event"
	eventdb "example.com/fixtures/features/create_event/db"
	"example.com/fixtures/features/create_invoice"
	createdb "example.com/fixtures/features/create_invoice/db"
	"example.com/fixtures/features/event_summary"
	summarydb "example.com/fixtures/features/event_summary/db"
	"example.com/fixtures/features/list_customer_invoices"
	listdb "example.com/fixtures/features/list_customer_invoices/db"
	"example.com/fixtures/features/my_events"
	myeventsdb "example.com/fixtures/features/my_events/db"
	"example.com/fixtures/features/release_example"
	releasedb "example.com/fixtures/features/release_example/db"
	"example.com/fixtures/features/rename_event"
	renamedb "example.com/fixtures/features/rename_event/db"
	"example.com/fixtures/features/rename_section"
	sectiondb "example.com/fixtures/features/rename_section/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// AppRoles is every role a signed-in user of this app can have (grammar
// A2). An action's Roles may only list these; bridge-en refuses a typo.
// "admin" bypasses ownership (A4): an action only admins may call may write
// owned tables (schema.sql "-- owner:"), and their child tables (A5),
// without limiting the write to the signed-in user's rows.
var AppRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")

// Routes wires every slice. One line per feature: Route constant -> Handle,
// bound with the slice's own Roles, so httpx.Bind checks who may call it
// before the action runs (A3). Queries go through txn.DB, so httpx.Bind runs
// each call in one transaction. identity is the app's sign-in hook; the
// sessions and passwords behind it stay in the app (nil: nobody is signed
// in, so only Public routes answer).
func Routes(db *sql.DB, identity httpx.Identity) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(add_section.Route, httpx.Bind(add_section.Roles, add_section.New(addsectiondb.New(txn.DB(db))).Handle))
	mux.Handle(admin_rename_event.Route, httpx.Bind(admin_rename_event.Roles, admin_rename_event.New(adminrenamedb.New(txn.DB(db))).Handle))
	mux.Handle(admin_rename_section.Route, httpx.Bind(admin_rename_section.Roles, admin_rename_section.New(adminsectiondb.New(txn.DB(db))).Handle))
	mux.Handle(claim_example.Route, httpx.Bind(claim_example.Roles, claim_example.New(claimdb.New(txn.DB(db))).Handle))
	mux.Handle(confirm_many.Route, httpx.Bind(confirm_many.Roles, confirm_many.New(confirmdb.New(txn.DB(db))).Handle))
	mux.Handle(create_event.Route, httpx.Bind(create_event.Roles, create_event.New(eventdb.New(txn.DB(db))).Handle))
	mux.Handle(create_invoice.Route, httpx.Bind(create_invoice.Roles, create_invoice.New(createdb.New(txn.DB(db))).Handle))
	mux.Handle(event_summary.Route, httpx.Bind(event_summary.Roles, event_summary.New(summarydb.New(txn.DB(db))).Handle))
	mux.Handle(list_customer_invoices.Route, httpx.Bind(list_customer_invoices.Roles, list_customer_invoices.New(listdb.New(txn.DB(db))).Handle))
	mux.Handle(my_events.Route, httpx.Bind(my_events.Roles, my_events.New(myeventsdb.New(txn.DB(db))).Handle))
	mux.Handle(release_example.Route, httpx.Bind(release_example.Roles, release_example.New(releasedb.New(txn.DB(db))).Handle))
	mux.Handle(rename_event.Route, httpx.Bind(rename_event.Roles, rename_event.New(renamedb.New(txn.DB(db))).Handle))
	mux.Handle(rename_section.Route, httpx.Bind(rename_section.Roles, rename_section.New(sectiondb.New(txn.DB(db))).Handle))
	return httpx.Identify(AppRoles, identity, mux)
}
