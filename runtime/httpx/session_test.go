package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests prove SessionRule and ServerSetWhen, the sentences bridge-en
// quotes for a `server:"session"` Input field (grammar T2), and that the
// clock field (T1) is refused from the query string too.

type sessionIn struct {
	SeatID  int64 `json:"seat_id"`
	Session int64 `json:"session" server:"session"`
	Now     int64 `json:"now" clock:"now"`
}

type sessionOut struct {
	Session int64 `json:"session"`
}

type textSessionIn struct {
	Session string `json:"session" server:"session"`
}

type textSessionOut struct {
	Session string `json:"session"`
}

// serve runs one request against a Bind of echo (records whether it ran).
func serveSession(t *testing.T, method, target, body string, cookies ...string) (*httptest.ResponseRecorder, bool, sessionOut) {
	t.Helper()
	ran := false
	h := Bind(func(_ context.Context, in sessionIn) (sessionOut, error) {
		ran = true
		return sessionOut{Session: in.Session}, nil
	})
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, rd)
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: c})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out sessionOut
	if rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("answer %s: %v", rec.Body, err)
		}
	}
	return rec, ran, out
}

// "set by the server from the session cookie `bridge_session`: its value
// when that is a whole number from 1 up"
func TestSessionRuleServerSetsSessionFromCookie(t *testing.T) {
	rec, ran, out := serveSession(t, http.MethodPost, "/x", `{"seat_id":7}`, "1001")
	if rec.Code != http.StatusCreated || !ran || out.Session != 1001 {
		t.Fatalf("status %d ran=%v session=%d: %s", rec.Code, ran, out.Session, rec.Body)
	}
}

// "or 0 when the request has no such cookie, has it more than once, or its
// value is anything else": the action runs with 0, so its own guard decides.
func TestSessionRuleZeroWithoutAValidCookie(t *testing.T) {
	for name, cookies := range map[string][]string{
		"no cookie":     nil,
		"twice":         {"1001", "1001"},
		"zero":          {"0"},
		"negative":      {"-5"},
		"plus sign":     {"+5"},
		"not a number":  {"abc"},
		"spaces":        {"1 2"},
		"overflow":      {"9223372036854775808"},
		"empty":         {""},
		"leading space": {" 7"},
	} {
		t.Run(name, func(t *testing.T) {
			rec, ran, out := serveSession(t, http.MethodPost, "/x", `{"seat_id":7}`, cookies...)
			if rec.Code != http.StatusCreated || !ran || out.Session != 0 {
				t.Fatalf("status %d ran=%v session=%d", rec.Code, ran, out.Session)
			}
		})
	}
	// Another cookie name is not the session.
	h := Bind(func(_ context.Context, in sessionIn) (sessionOut, error) { return sessionOut{Session: in.Session}, nil })
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"seat_id":7}`))
	req.AddCookie(&http.Cookie{Name: "session", Value: "42"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"session":0`) {
		t.Fatalf("other cookie used: %s", rec.Body)
	}
}

// "the caller does not send it, and a request that does is answered with
// HTTP 400 below" (ServerSetWhen: in the body or in the query string, in any
// letter case), for the session and the clock alike.
func TestServerSetCallerCannotSendIt(t *testing.T) {
	for name, tc := range map[string]struct{ method, target, body, want string }{
		"session in body":            {http.MethodPost, "/x", `{"seat_id":7,"session":1001}`, `"session" is set by the server (the session cookie bridge_session)`},
		"session in body, any case":  {http.MethodPost, "/x", `{"seat_id":7,"SESSION":1001}`, `"session" is set by the server`},
		"session null in body":       {http.MethodPost, "/x", `{"seat_id":7,"session":null}`, `"session" is set by the server`},
		"session in query":           {http.MethodPost, "/x?session=1001", `{"seat_id":7}`, `"session" is set by the server`},
		"session in query, any case": {http.MethodPost, "/x?Session=1001", `{"seat_id":7}`, `"session" is set by the server`},
		"now in query":               {http.MethodPost, "/x?now=1", `{"seat_id":7}`, `"now" is set by the server (the current time)`},
		"now in body, any case":      {http.MethodPost, "/x", `{"seat_id":7,"Now":1}`, `"now" is set by the server`},
	} {
		t.Run(name, func(t *testing.T) {
			rec, ran, _ := serveSession(t, tc.method, tc.target, tc.body, "1001")
			var eb ErrorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &eb)
			if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || !strings.Contains(eb.Error.Message, tc.want) || ran {
				t.Fatalf("status %d ran=%v body %s", rec.Code, ran, rec.Body)
			}
		})
	}
}

// GET: the session comes from the cookie; the query string cannot set it.
func TestSessionRuleOnGET(t *testing.T) {
	type getIn struct {
		ID      int64 `json:"id" path:"id"`
		Session int64 `json:"session" server:"session"`
		Now     int64 `json:"now" clock:"now"`
	}
	var got getIn
	ran := false
	mux := http.NewServeMux()
	mux.Handle("GET /x/{id}", Bind(func(_ context.Context, in getIn) (sessionOut, error) { ran, got = true, in; return sessionOut{}, nil }))
	req := httptest.NewRequest(http.MethodGet, "/x/3", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "77"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || got.ID != 3 || got.Session != 77 {
		t.Fatalf("status %d got %+v", rec.Code, got)
	}
	for _, target := range []string{"/x/3?session=77", "/x/3?now=1", "/x/3?SESSION=1"} {
		ran = false
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "77"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || ran || !strings.Contains(rec.Body.String(), "is set by the server") {
			t.Fatalf("%s: status %d ran=%v %s", target, rec.Code, ran, rec.Body)
		}
	}
}

// A GET whose only inputs are server-set takes no body.
func TestSessionOnlyGET(t *testing.T) {
	type onlyIn struct {
		Session int64 `json:"session" server:"session"`
	}
	mux := http.NewServeMux()
	mux.Handle("GET /me", Bind(func(_ context.Context, in onlyIn) (sessionOut, error) { return sessionOut{Session: in.Session}, nil }))
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "5"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"session":5`) {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
}

// Text sessions: "1 to 128 letters, digits, '-', '_' or '.'", else "the
// empty text".
func TestSessionRuleText(t *testing.T) {
	h := Bind(func(_ context.Context, in textSessionIn) (textSessionOut, error) {
		return textSessionOut{Session: in.Session}, nil
	})
	for value, want := range map[string]string{
		"abc-DEF_1.2":            "abc-DEF_1.2",
		strings.Repeat("a", 128): strings.Repeat("a", 128),
		strings.Repeat("a", 129): "",
		"a b":                    "",
		"a%20b":                  "",
		"":                       "",
		"\u00e9":                 "",
		"a/b":                    "",
	} {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: value})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out textSessionOut
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusCreated || out.Session != want {
			t.Fatalf("cookie %q: status %d %s, want %q", value, rec.Code, rec.Body, want)
		}
	}
}

// The English names the cookie and both values.
func TestSessionRuleNamesTheCookie(t *testing.T) {
	if !strings.Contains(SessionRule, "`"+SessionCookie+"`") || !strings.Contains(SessionRule, "{zero}") || !strings.Contains(SessionRule, "{valid}") {
		t.Fatalf("SessionRule: %s", SessionRule)
	}
	for _, typ := range []string{"int64", "string"} {
		if v := SessionValue[typ]; v.Valid == "" || v.Zero == "" {
			t.Fatalf("SessionValue[%s] = %+v", typ, v)
		}
	}
}

// A server tag of another shape is a bug in the slice: HTTP 500, not run.
func TestServerTagOfAnotherShapeIsABug(t *testing.T) {
	type badIn struct {
		User int64 `json:"user" server:"user"`
	}
	ran := false
	rec := httptest.NewRecorder()
	Bind(func(_ context.Context, in badIn) (sessionOut, error) { ran = true; return sessionOut{}, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`)))
	if rec.Code != Internal.Status || ran {
		t.Fatalf("status %d ran=%v", rec.Code, ran)
	}
}
