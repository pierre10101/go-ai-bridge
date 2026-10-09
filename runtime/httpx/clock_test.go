package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests prove ClockRule, the sentence bridge-en quotes for a
// `clock:"now"` Input field (grammar T1).

type clockIn struct {
	SeatID int64 `json:"seat_id"`
	Now    int64 `json:"now" clock:"now"`
}

type clockOut struct {
	Now int64 `json:"now"`
}

func echoNow(_ context.Context, in clockIn) (clockOut, error) { return clockOut{Now: in.Now}, nil }

func withClock(t *testing.T, at time.Time) {
	t.Helper()
	old := Now
	Now = func() time.Time { return at }
	t.Cleanup(func() { Now = old })
}

// "set by the server to the current time when the request arrives, in whole
// seconds since 1970-01-01 UTC; the caller does not send it"
func TestClockRuleServerSetsNow(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 999, time.UTC)
	withClock(t, at)
	rec := httptest.NewRecorder()
	Bind(Public, echoNow).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"seat_id":7}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out clockOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Now != at.Unix() {
		t.Fatalf("now = %d, want %d (%v)", out.Now, at.Unix(), err)
	}
}

// "a request that does is answered with HTTP 400 below"
func TestClockRuleCallerCannotSendNow(t *testing.T) {
	ran := false
	rec := httptest.NewRecorder()
	Bind(Public, func(ctx context.Context, in clockIn) (clockOut, error) { ran = true; return echoNow(ctx, in) }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"seat_id":7,"now":1}`)))
	var eb ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || !strings.Contains(eb.Error.Message, `"now" is set by the server`) || ran {
		t.Fatalf("status %d body %s ran=%v", rec.Code, rec.Body, ran)
	}
}

// The clock field is not a required body field: leaving it out is fine,
// while other fields stay required (InputRule).
func TestClockRuleOtherFieldsStillRequired(t *testing.T) {
	rec := httptest.NewRecorder()
	Bind(Public, echoNow).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `required field \"seat_id\" is missing`) ||
		strings.Contains(rec.Body.String(), `"now\"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

// GET inputs get the server time too.
func TestClockRuleOnGET(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	withClock(t, at)
	type getIn struct {
		ID  int64 `json:"id" path:"id"`
		Now int64 `json:"now" clock:"now"`
	}
	var got getIn
	mux := http.NewServeMux()
	mux.Handle("GET /x/{id}", Bind(Public, func(_ context.Context, in getIn) (clockOut, error) { got = in; return clockOut{}, nil }))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x/3", nil))
	if rec.Code != http.StatusOK || got.ID != 3 || got.Now != at.Unix() {
		t.Fatalf("status %d got %+v", rec.Code, got)
	}
}
