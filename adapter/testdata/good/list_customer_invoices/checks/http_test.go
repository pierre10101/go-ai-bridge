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

func serveListOn(conn *sql.DB, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle(list_customer_invoices.Route, httpx.Bind(list_customer_invoices.New(db.New(txn.DB(conn))).Handle))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
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
