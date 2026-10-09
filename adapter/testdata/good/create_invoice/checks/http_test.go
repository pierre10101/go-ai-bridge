package checks

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/fixtures/features/create_invoice"
	"example.com/fixtures/features/create_invoice/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func serve(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	_, conn := newAction(t)
	return serveOn(conn, body)
}

// serveOn wires the slice exactly like cmd/server/routes.go.
func serveOn(conn *sql.DB, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle(create_invoice.Route, httpx.Bind(create_invoice.New(db.New(txn.DB(conn))).Handle))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoices", strings.NewReader(body)))
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorBody {
	t.Helper()
	var body httpx.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	return body
}

func TestHTTPCreateInvoiceReturns201(t *testing.T) {
	rec := serve(t, `{"customer_id":1,"amount_cents":1500,"currency":"ZAR"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out create_invoice.Output
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.InvoiceNumber != "INV-000001" {
		t.Fatalf("body %s (%v)", rec.Body, err)
	}
}

func TestF2_HTTPReturns422WithFailureID(t *testing.T) {
	rec := serve(t, `{"customer_id":1,"amount_cents":0,"currency":"ZAR"}`)
	var body httpx.ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != create_invoice.F2.Status || body.Error.ID != create_invoice.F2.ID || body.Error.Message != create_invoice.F2.Message {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestHTTPRejectsUnknownFields(t *testing.T) {
	rec := serve(t, `{"customer_id":1,"amount_cents":1,"currency":"ZAR","discount":5}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

// Required fields are checked at the door: a request that leaves out
// customer_id, or sends it as null, is HTTP 400, not F1 (and not F2 for a
// missing amount_cents). The action does not run, so nothing is written.
func TestHTTPMissingOrNullFieldIs400(t *testing.T) {
	cases := map[string]string{
		`{"amount_cents":1500,"currency":"ZAR"}`:                    `required field "customer_id" is missing or null`,
		`{"customer_id":null,"amount_cents":1500,"currency":"ZAR"}`: `required field "customer_id" is missing or null`,
		`{"customer_id":1,"currency":"ZAR"}`:                        `required field "amount_cents" is missing or null`,
		`{}`:                                                        `required fields "customer_id", "amount_cents", "currency" are missing or null`,
		`{"Customer_ID":1,"amount_cents":1500,"currency":"ZAR"}`:    `json: unknown field "Customer_ID" (field names are case-sensitive)`,
	}
	for body, want := range cases {
		_, conn := newAction(t)
		rec := serveOn(conn, body)
		eb := errorBody(t, rec)
		if rec.Code != httpx.BadInput.Status || eb.Error.ID != httpx.BadInput.ID || eb.Error.Message != want {
			t.Errorf("%s: status %d %s; want 400 %q", body, rec.Code, rec.Body, want)
		}
		if countInvoices(t, conn) != 0 {
			t.Errorf("%s: a bad request must not write", body)
		}
	}
}

// A 500 after the insert (step 6 assertion) leaves no stored invoice behind.
func TestHTTPInternalErrorAfterInsertWritesNothing(t *testing.T) {
	_, conn := newAction(t)
	if _, err := conn.Exec(`INSERT INTO invoices (seq, customer_id, amount_cents, currency) VALUES (999999, 1, 100, 'ZAR')`); err != nil {
		t.Fatal(err)
	}
	rec := serveOn(conn, `{"customer_id":1,"amount_cents":1500,"currency":"ZAR"}`)
	if eb := errorBody(t, rec); rec.Code != httpx.Internal.Status || eb.Error.ID != httpx.Internal.ID {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if countInvoices(t, conn) != 1 {
		t.Fatalf("invoices %d: the 500 must not leave the new invoice stored", countInvoices(t, conn))
	}
}
