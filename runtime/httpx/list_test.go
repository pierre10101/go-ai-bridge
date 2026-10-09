package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// These tests prove ListRule, ListRuleExact, ListElems and ListWhen (D10).

type idsIn struct {
	IDs     []int64 `json:"ids" list:"1..3"`
	Session int64   `json:"session" server:"session"`
}

type textListIn struct {
	Codes []string `json:"codes" list:"2..2"`
}

func serveList[I any](t *testing.T, body string) (*httptest.ResponseRecorder, ErrorBody, *I) {
	t.Helper()
	var got *I
	rec := httptest.NewRecorder()
	h := Bind(func(_ context.Context, in I) (out, error) { got = &in; return out{OK: true}, nil })
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	var eb ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	return rec, eb, got
}

// ListRule: a list of {min} to {max} entries with no duplicates is accepted
// as sent, in order.
func TestListRuleAccepts(t *testing.T) {
	for body, want := range map[string]string{
		`{"ids":[7]}`:       "[7]",
		`{"ids":[3,1,2]}`:   "[3 1 2]",
		`{"ids":[ 1 , 2 ]}`: "[1 2]",
	} {
		rec, eb, got := serveList[idsIn](t, body)
		if rec.Code != http.StatusCreated || got == nil {
			t.Fatalf("%s: %d %+v", body, rec.Code, eb)
		}
		if fmt.Sprint(got.IDs) != want {
			t.Fatalf("%s: got %v", body, got.IDs)
		}
	}
	rec, eb, got := serveList[textListIn](t, `{"codes":["a","b"]}`)
	if rec.Code != http.StatusCreated || got == nil || strings.Join(got.Codes, ",") != "a,b" {
		t.Fatalf("text list: %d %+v %v", rec.Code, eb, got)
	}
}

// silence keeps the expected "BUG:" log lines out of the test output.
func silence(t *testing.T) {
	t.Helper()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// ListWhen and BadInput.When: empty, too long, a repeated entry, a null
// entry, an entry of the wrong type, or no list at all is HTTP 400
// bad_request, and the action does not run.
func TestListWhenRefuses(t *testing.T) {
	cases := map[string]struct{ body, msg string }{
		"empty":         {`{"ids":[]}`, `field "ids" must have 1 to 3 entries, not 0`},
		"too long":      {`{"ids":[1,2,3,4]}`, `field "ids" must have 1 to 3 entries, not 4`},
		"duplicate":     {`{"ids":[1,2,1]}`, `field "ids" has the entry "1" more than once`},
		"null entry":    {`{"ids":[1,null]}`, `field "ids" has an entry that is null`},
		"text entry":    {`{"ids":[1,"2"]}`, `cannot unmarshal string`},
		"float entry":   {`{"ids":[1.5]}`, `cannot unmarshal number 1.5`},
		"bool entry":    {`{"ids":[true]}`, `cannot unmarshal bool`},
		"object entry":  {`{"ids":[{"id":1}]}`, `cannot unmarshal object`},
		"nested list":   {`{"ids":[[1]]}`, `cannot unmarshal array`},
		"not a list":    {`{"ids":1}`, `cannot unmarshal number`},
		"null list":     {`{"ids":null}`, `required field "ids" is missing or null`},
		"left out":      {`{}`, `required field "ids" is missing or null`},
		"session sent":  {`{"ids":[1],"session":7}`, `set by the server`},
		"overflow":      {`{"ids":[9223372036854775808]}`, `cannot unmarshal number`},
		"unknown field": {`{"ids":[1],"other":[1]}`, `unknown field`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec, eb, got := serveList[idsIn](t, tc.body)
			if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || got != nil {
				t.Fatalf("status %d id %q ran %v", rec.Code, eb.Error.ID, got != nil)
			}
			if !strings.Contains(eb.Error.Message, tc.msg) {
				t.Fatalf("message %q, want %q", eb.Error.Message, tc.msg)
			}
		})
	}
	for name, body := range map[string]string{
		"text too short": `{"codes":["a"]}`,
		"text duplicate": `{"codes":["a","a"]}`,
		"text null":      `{"codes":["a",null]}`,
		"text number":    `{"codes":["a",1]}`,
	} {
		rec, eb, got := serveList[textListIn](t, body)
		if rec.Code != BadInput.Status || eb.Error.ID != BadInput.ID || got != nil {
			t.Fatalf("%s: status %d id %q ran %v", name, rec.Code, eb.Error.ID, got != nil)
		}
	}
}

// A slice field without a valid list tag is a bug in the slice (bridge-en
// refuses it too): Bind answers HTTP 500 and the action does not run.
func TestListTagIsRequired(t *testing.T) {
	type untagged struct {
		IDs []int64 `json:"ids"`
	}
	type badTag struct {
		IDs []int64 `json:"ids" list:"0..5"`
	}
	type badElem struct {
		IDs []bool `json:"ids" list:"1..5"`
	}
	silence(t)
	if rec, _, got := serveList[untagged](t, `{"ids":[1]}`); rec.Code != Internal.Status || got != nil {
		t.Fatalf("untagged: %d", rec.Code)
	}
	if rec, _, got := serveList[badTag](t, `{"ids":[1]}`); rec.Code != Internal.Status || got != nil {
		t.Fatalf("bad tag: %d", rec.Code)
	}
	if rec, _, got := serveList[badElem](t, `{"ids":[true]}`); rec.Code != Internal.Status || got != nil {
		t.Fatalf("bad element: %d", rec.Code)
	}
}

func TestParseListTag(t *testing.T) {
	for tag, want := range map[string][2]int{"1..20": {1, 20}, "3..3": {3, 3}, "1..100": {1, 100}} {
		min, max, err := ParseListTag(tag)
		if err != nil || min != want[0] || max != want[1] {
			t.Fatalf("%s: %d %d %v", tag, min, max, err)
		}
	}
	for _, tag := range []string{"", "20", "0..5", "5..1", "1..101", "-1..5", "1..2..3", " 1..5", "1..5 ", "a..b", "1.. 5", "+1..5"} {
		if _, _, err := ParseListTag(tag); err == nil {
			t.Fatalf("%q parsed", tag)
		}
	}
}

// ListRule and ListRuleExact say exactly what Bind enforces: the bounds and
// the element words come from the tag and ListElems.
func TestListRuleWords(t *testing.T) {
	if !strings.Contains(ListRule, "{min}") || !strings.Contains(ListRule, "{max}") || !strings.Contains(ListRule, "{elems}") ||
		!strings.Contains(ListRuleExact, "{min}") || !strings.Contains(ListRuleExact, "{elems}") {
		t.Fatal("ListRule/ListRuleExact lost a placeholder")
	}
	if len(ListElems) != 2 || ListElems["int64"] == "" || ListElems["string"] == "" {
		t.Fatalf("ListElems %v", ListElems)
	}
	if !strings.Contains(ListRule, "no duplicates") || !strings.Contains(ListWhen, "twice") || !strings.Contains(ListWhen, "null") {
		t.Fatal("ListRule/ListWhen no longer name duplicates and null entries")
	}
}
