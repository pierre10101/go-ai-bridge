package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/page"
)

// These tests prove every sentence bridge-en quotes from this package.

type in struct {
	N    int64  `json:"n"`
	Text string `json:"text"`
	Flag bool   `json:"flag"`
}

// full sets every field of in; Bind requires them all (InputRule).
const full = `{"n":1,"text":"t","flag":false}`

type out struct {
	OK bool `json:"ok"`
}

var errBoom = failure.New("F9", http.StatusConflict, "boom")

func serve(t *testing.T, method, body string, handle func(context.Context, in) (out, error)) (*httptest.ResponseRecorder, ErrorBody, string) {
	t.Helper()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	rec := httptest.NewRecorder()
	Bind(Public, handle).ServeHTTP(rec, httptest.NewRequest(method, "/x", strings.NewReader(body)))
	var eb ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	return rec, eb, logs.String()
}

func ok(context.Context, in) (out, error) { return out{OK: true}, nil }

// BadInput.When: not exactly one JSON object, a field left out or null, unknown
// field (case-sensitive), wrong type, > 1 MiB. Note: the action does not run.
func TestBadInput(t *testing.T) {
	cases := map[string]string{
		"not JSON":      `{"n":`,
		"two values":    `{"n":1}{"n":2}`,
		"trailing junk": `{"n":1} x`,
		"array":         `[1]`,
		"null body":     `null`,
		"number body":   `3`,
		"empty body":    ``,
		"unknown field": `{"n":1,"text":"t","flag":true,"extra":true}`,
		"wrong case":    `{"N":1,"text":"t","flag":true}`,
		"wrong type":    `{"n":"one","text":"t","flag":true}`,
		"left out":      `{"n":1,"text":"t"}`,
		"null":          `{"n":1,"text":null,"flag":true}`,
		"too large":     `{"n":1,"flag":true,"text":"` + strings.Repeat("a", MaxBody) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			ran := false
			rec, eb, _ := serve(t, http.MethodPost, body, func(context.Context, in) (out, error) { ran = true; return out{}, nil })
			if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || eb.Error.Message == "" || ran {
				t.Fatalf("status %d id %q msg %q ran %v", rec.Code, eb.Error.ID, eb.Error.Message, ran)
			}
		})
	}
}

// InputRule: every field is required, also inside objects; a field left out
// or null is HTTP 400 and the action does not run. Zero values that are sent
// are values, not missing fields.
func TestInputRule(t *testing.T) {
	cases := map[string]string{
		`{}`:                                 `required fields "n", "text", "flag" are missing or null`,
		`{"n":null,"text":null,"flag":null}`: `required fields "n", "text", "flag" are missing or null`,
		`{"text":"t","flag":true}`:           `required field "n" is missing or null`,
		`{"n":1,"text":"t","flag":true,"Flag":false}`: `json: unknown field "Flag" (field names are case-sensitive)`,
	}
	for body, want := range cases {
		ran := false
		rec, eb, _ := serve(t, http.MethodPost, body, func(context.Context, in) (out, error) { ran = true; return out{}, nil })
		if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || eb.Error.Message != want || ran {
			t.Errorf("%s: status %d id %q msg %q ran %v; want msg %q", body, rec.Code, eb.Error.ID, eb.Error.Message, ran, want)
		}
	}
	var got in
	rec, _, _ := serve(t, http.MethodPost, `{"n":0,"text":"","flag":false}`, func(_ context.Context, i in) (out, error) { got = i; return out{}, nil })
	if rec.Code != http.StatusCreated || got != (in{}) {
		t.Fatalf("zero values sent explicitly: status %d input %+v", rec.Code, got)
	}
}

// InputRule "also inside objects": a nested struct field (like domain.Money) is required field by field.
func TestInputRuleNested(t *testing.T) {
	type money struct {
		Cents    int64  `json:"cents"`
		Currency string `json:"currency"`
	}
	type withMoney struct {
		Total money `json:"total"`
	}
	for body, want := range map[string]string{
		`{}`:                                   `required field "total" is missing or null`,
		`{"total":null}`:                       `required field "total" is missing or null`,
		`{"total":{"cents":5}}`:                `required field "total.currency" is missing or null`,
		`{"total":{"cents":5,"Currency":"X"}}`: `json: unknown field "total.Currency" (field names are case-sensitive)`,
	} {
		rec := httptest.NewRecorder()
		Bind(Public, func(context.Context, withMoney) (out, error) { return out{}, nil }).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
		var eb ErrorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &eb)
		if rec.Code != http.StatusBadRequest || eb.Error.Message != want {
			t.Errorf("%s: status %d msg %q, want %q", body, rec.Code, eb.Error.Message, want)
		}
	}
}

// Internal: a non-failure error answers 500 "internal error"; the cause is logged, never sent.
func TestInternalOnError(t *testing.T) {
	rec, eb, logs := serve(t, http.MethodPost, full, func(context.Context, in) (out, error) {
		return out{}, errors.New("secret database detail")
	})
	if rec.Code != Internal.Status || eb.Error.ID != Internal.ID || eb.Error.Message != Internal.Message {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret") || !strings.Contains(logs, "secret database detail") {
		t.Fatalf("cause must be logged, not sent: body %s logs %q", rec.Body, logs)
	}
}

// Internal: a failed assertion answers the same way.
func TestInternalOnViolation(t *testing.T) {
	rec, eb, logs := serve(t, http.MethodPost, full, func(context.Context, in) (out, error) {
		assert.Post(false, "secret invariant")
		return out{}, nil
	})
	if rec.Code != Internal.Status || eb.Error.Message != Internal.Message || strings.Contains(rec.Body.String(), "secret") || !strings.Contains(logs, "secret invariant") {
		t.Fatalf("status %d body %s logs %q", rec.Code, rec.Body, logs)
	}
}

// F-ID failures answer their own status, id and message.
func TestFailure(t *testing.T) {
	rec, eb, _ := serve(t, http.MethodPost, full, func(context.Context, in) (out, error) { return out{}, errBoom })
	if rec.Code != errBoom.Status || eb.Error.ID != errBoom.ID || eb.Error.Message != errBoom.Message {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

// SuccessStatus: every route method answers its declared status.
func TestSuccessStatus(t *testing.T) {
	for method, want := range SuccessStatus {
		rec, _, _ := serve(t, method, full, ok)
		if rec.Code != want {
			t.Fatalf("%s: status %d, want %d", method, rec.Code, want)
		}
	}
	if SuccessStatus[http.MethodPost] != http.StatusCreated {
		t.Fatal("POST must answer 201 Created")
	}
}

type listIn struct {
	CustomerID int64 `json:"customer_id" path:"id"`
	After      int64 `json:"after" query:"after"`
	Limit      int64 `json:"limit" query:"limit"`
}

func TestGETPathQueryDefaults(t *testing.T) {
	mux := http.NewServeMux()
	var got listIn
	mux.Handle("GET /customers/{id}/invoices", Bind(Public, func(_ context.Context, in listIn) (out, error) {
		got = in
		return out{OK: true}, nil
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers/7/invoices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got.CustomerID != 7 || got.Limit != page.DefaultPageSize || got.After != page.StartCursor {
		t.Fatalf("got %+v", got)
	}
}

// after=0 is passed through as 0 (not remapped to StartCursor). next_after: 0
// means "last page"; a client that reused it as after would loop forever if
// Bind treated 0 as the start of the list. Omit after for the first page.
func TestGETAfterZeroIsPassedThrough(t *testing.T) {
	mux := http.NewServeMux()
	var got listIn
	mux.Handle("GET /customers/{id}/invoices", Bind(Public, func(_ context.Context, in listIn) (out, error) {
		got = in
		return out{OK: true}, nil
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers/7/invoices?after=0", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got.After != 0 {
		t.Fatalf("after=0: got %d, want 0 (not StartCursor)", got.After)
	}
}

type patchIn struct {
	EventID int64  `json:"event_id" path:"id"`
	Title   string `json:"title"`
}

// path:"..." on PATCH is filled from the URL; a body that also carries it is 400.
func TestPATCHPathFromURL(t *testing.T) {
	mux := http.NewServeMux()
	var got patchIn
	mux.Handle("PATCH /events/{id}/title", Bind(Public, func(_ context.Context, in patchIn) (out, error) {
		got = in
		return out{OK: true}, nil
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/events/9/title", strings.NewReader(`{"title":"Launch"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got.EventID != 9 || got.Title != "Launch" {
		t.Fatalf("got %+v", got)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/events/9/title", strings.NewReader(`{"event_id":3,"title":"x"}`)))
	if rec.Code != BadInput.Status {
		t.Fatalf("body with path field: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(PathBodyRule, "not sent in the body") || !strings.Contains(PathBodyWhen, "comes from the path") {
		t.Fatal("PathBodyRule and PathBodyWhen must say what required does for path fields")
	}
}

type pathOnlyIn struct {
	EventID int64 `json:"event_id" path:"id"`
}

// A path-only non-GET still needs a JSON body: {} succeeds; a missing body is 400.
func TestPATCHPathOnlyEmptyBody(t *testing.T) {
	mux := http.NewServeMux()
	var got pathOnlyIn
	mux.Handle("DELETE /events/{id}", Bind(Public, func(_ context.Context, in pathOnlyIn) (out, error) {
		got = in
		return out{OK: true}, nil
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/events/9", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("{}: status %d: %s", rec.Code, rec.Body)
	}
	if got.EventID != 9 {
		t.Fatalf("got %+v", got)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/events/9", nil))
	if rec.Code != BadInput.Status {
		t.Fatalf("missing body: status %d: %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/events/9", strings.NewReader(`{"event_id":3}`)))
	if rec.Code != BadInput.Status {
		t.Fatalf("body with path field: status %d: %s", rec.Code, rec.Body)
	}
}

func TestGETBadLimitIs400(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /customers/{id}/invoices", Bind(Public, func(_ context.Context, in listIn) (out, error) {
		return out{OK: true}, nil
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/customers/1/invoices?limit=nope", nil))
	if rec.Code != BadInput.Status {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

// StrictQueryRule (T4) and BadQueryWhen: a GET whose query string has a
// parameter the action does not declare, in any letter case other than the
// declared one, is answered with BadInput and the action does not run; the
// declared ones still work. A GET that declares no query value refuses any.
func TestStrictQueryRule(t *testing.T) {
	ran := false
	mux := http.NewServeMux()
	mux.Handle("GET /customers/{id}/invoices", Bind(Public, func(_ context.Context, in listIn) (out, error) {
		ran = true
		return out{OK: true}, nil
	}))
	type none struct {
		User int64 `json:"user" server:"user"`
	}
	mux.Handle("GET /me", Bind(Public, func(_ context.Context, in none) (out, error) {
		ran = true
		return out{OK: true}, nil
	}))
	for _, url := range []string{
		"/customers/1/invoices?limit=5&debug=1",
		"/customers/1/invoices?Limit=5",
		"/customers/1/invoices?customer_id=2",
		"/customers/1/invoices?id=2",
		"/me?x=1",
		"/me?limit=1",
	} {
		ran = false
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != BadInput.Status || ran {
			t.Errorf("%s: status %d, ran %v: %s", url, rec.Code, ran, rec.Body)
		}
		var eb ErrorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &eb)
		if eb.Error.ID != BadInput.ID || !strings.Contains(eb.Error.Message, "is not one this action takes") {
			t.Errorf("%s: body %s", url, rec.Body)
		}
	}
	for _, url := range []string{"/customers/1/invoices", "/customers/1/invoices?limit=5&after=9", "/me"} {
		ran = false
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK || !ran {
			t.Errorf("%s: status %d, ran %v: %s", url, rec.Code, ran, rec.Body)
		}
	}
	if !strings.Contains(BadQueryWhen, "a parameter not listed above") || !strings.Contains(StrictQueryRule, "a query parameter that is not listed above") {
		t.Fatal("BadQueryWhen and StrictQueryRule must say what unknownQuery does")
	}
}
