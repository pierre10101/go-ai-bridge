package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/fixtures/features/list_customer_invoices"
	"example.com/fixtures/features/list_customer_invoices/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func serveList(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	_, conn := newAction(t)
	return serveListOn(conn, path)
}

// serveListOn calls the route signed in as finance staff, the way
// cmd/server/routes.go wires it.
func serveListOn(conn *sql.DB, path string) *httptest.ResponseRecorder {
	return serveListAs(conn, path, "finance")
}

// appRoles is the list cmd/server/routes.go declares (AppRoles).
var appRoles = httpx.AppRoles("customer", "organizer", "finance", "admin")

// serveListAs wires the slice exactly like cmd/server/routes.go, with a
// sign-in hook that says the caller is user 5 in role ("" = not signed in).
func serveListAs(conn *sql.DB, path, role string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle(list_customer_invoices.Route, httpx.Bind(list_customer_invoices.Roles, list_customer_invoices.New(db.New(txn.DB(conn))).Handle))
	identity := func(*http.Request) (string, string, bool) { return "5", role, role != "" }
	rec := httptest.NewRecorder()
	httpx.Identify(appRoles, identity, mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// Only finance staff and admins may list invoices: 401 when not signed in,
// 403 for any other role, before the action runs (no read-only transaction
// is opened, so a bad page limit is not even looked at).
func TestHTTPListOnlyFinanceAndAdmin(t *testing.T) {
	_, conn := newAction(t)
	seedInvoices(t, conn, 1)
	for role, want := range map[string]httpx.Outcome{"": httpx.Unauthenticated, "customer": httpx.Forbidden, "organizer": httpx.Forbidden} {
		for _, path := range []string{"/customers/1/invoices", "/customers/1/invoices?limit=0"} {
			rec := serveListAs(conn, path, role)
			var body httpx.ErrorBody
			if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != want.Status || body.Error.ID != want.ID {
				t.Errorf("role %q %s: status %d %s", role, path, rec.Code, rec.Body)
			}
		}
	}
	for _, role := range []string{"finance", "admin"} {
		if rec := serveListAs(conn, "/customers/1/invoices", role); rec.Code != http.StatusOK {
			t.Errorf("role %s: status %d %s", role, rec.Code, rec.Body)
		}
	}
}

func TestHTTPListReturns200(t *testing.T) {
	_, conn := newAction(t)
	seedInvoices(t, conn, 3)
	rec := serveListOn(conn, "/customers/1/invoices?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out list_customer_invoices.Output
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Invoices) != 2 {
		t.Fatalf("body %s (%v)", rec.Body, err)
	}
	if string(out.Invoices[0].InvoiceNumber) != "INV-000003" {
		t.Fatalf("newest first: %+v", out.Invoices)
	}
}

func TestHTTPListDefaultsLimitAndAfter(t *testing.T) {
	_, conn := newAction(t)
	seedInvoices(t, conn, 25)
	rec := serveListOn(conn, "/customers/1/invoices")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out list_customer_invoices.Output
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Invoices) != int(page.DefaultPageSize) {
		t.Fatalf("default page size: got %d want %d", len(out.Invoices), page.DefaultPageSize)
	}
}

func TestF1_HTTPReturns422(t *testing.T) {
	rec := serveList(t, "/customers/99/invoices")
	var body httpx.ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != list_customer_invoices.F1.Status || body.Error.ID != list_customer_invoices.F1.ID {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestF2_HTTPReturns400(t *testing.T) {
	rec := serveList(t, "/customers/1/invoices?limit=101")
	var body httpx.ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != list_customer_invoices.F2.Status || body.Error.ID != list_customer_invoices.F2.ID {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestHTTPBadQueryParamIs400(t *testing.T) {
	rec := serveList(t, "/customers/1/invoices?limit=abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}
