package checks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/fixtures/features/confirm_many"
	"example.com/fixtures/features/confirm_many/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func serve(t *testing.T) (func(cookie, target, body string) *httptest.ResponseRecorder, func() []ticket) {
	t.Helper()
	_, conn := newAction(t, seed)
	now := httpx.Now
	httpx.Now = func() time.Time { return time.Unix(t0, 0) }
	t.Cleanup(func() { httpx.Now = now })
	mux := http.NewServeMux()
	mux.Handle(confirm_many.Route, httpx.Bind(confirm_many.New(db.New(txn.DB(conn))).Handle))
	post := func(cookie, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	return post, func() []ticket { return tickets(t, conn) }
}

// D10 over HTTP: an empty list, more than 20 ids, a repeated id, an id that
// is not a whole number, a null id, no list, or a caller-sent session or
// time is HTTP 400 bad_request; the action does not run, nothing is written.
func TestHTTPListInputIsChecked(t *testing.T) {
	post, rows := serve(t)
	ids21 := strings.TrimSuffix(strings.Repeat("1,", 21), ",")
	for _, r := range []struct{ target, body string }{
		{"/tickets/confirm", `{"ticket_ids": []}`},
		{"/tickets/confirm", `{"ticket_ids": [` + ids21 + `]}`},
		{"/tickets/confirm", `{"ticket_ids": [1, 2, 1]}`},
		{"/tickets/confirm", `{"ticket_ids": [1, "2"]}`},
		{"/tickets/confirm", `{"ticket_ids": [1, 2.5]}`},
		{"/tickets/confirm", `{"ticket_ids": [1, null]}`},
		{"/tickets/confirm", `{"ticket_ids": null}`},
		{"/tickets/confirm", `{}`},
		{"/tickets/confirm", `{"ticket_ids": [1], "session": "s7"}`},
		{"/tickets/confirm", `{"ticket_ids": [1], "now": 1}`},
		{"/tickets/confirm?session=s7", `{"ticket_ids": [1]}`},
	} {
		if rec := post("s7", r.target, r.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"bad_request"`) {
			t.Fatalf("%s %s: %d %s", r.target, r.body, rec.Code, rec.Body)
		}
	}
	for i, r := range rows() {
		if r != seed[i] {
			t.Fatalf("ticket %+v changed by a refused request", r)
		}
	}
	if rec := post("s7", "/tickets/confirm", `{"ticket_ids": [1, 2, 3]}`); rec.Code != http.StatusCreated || strings.TrimSpace(rec.Body.String()) != `{"confirmed":3}` {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}
}

// Without a valid session cookie the session is the empty text: F1.
func TestF1_HTTPNoCookie(t *testing.T) {
	post, rows := serve(t)
	for _, cookie := range []string{"", "not valid!"} {
		rec := post(cookie, "/tickets/confirm", `{"ticket_ids": [1]}`)
		if rec.Code != confirm_many.F1.Status || !strings.Contains(rec.Body.String(), `"`+confirm_many.F1.ID+`"`) {
			t.Fatalf("cookie %q: %d %s", cookie, rec.Code, rec.Body)
		}
	}
	if rows()[0] != seed[0] {
		t.Fatal("ticket 1 changed")
	}
}

// Over HTTP an expired hold is F2 (410) and a foreign ticket F3 (409).
func TestF2F3_HTTPStatuses(t *testing.T) {
	post, _ := serve(t)
	if rec := post("s7", "/tickets/confirm", `{"ticket_ids": [1, 4]}`); rec.Code != confirm_many.F3.Status {
		t.Fatalf("foreign ticket: %d %s", rec.Code, rec.Body)
	}
	httpx.Now = func() time.Time { return time.Unix(t0+600, 0) }
	if rec := post("s7", "/tickets/confirm", `{"ticket_ids": [1, 2]}`); rec.Code != confirm_many.F2.Status {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body)
	}
}
