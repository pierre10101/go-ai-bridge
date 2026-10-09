package page

import (
	"testing"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

type row struct {
	Seq         int64
	AmountCents int64
}

// "the `seq` of the last listed row if the page is full (it has <limit> of
// them), otherwise 0: there is no next page."
func TestNextAfter(t *testing.T) {
	full := []row{{Seq: 9}, {Seq: 7}, {Seq: 4}}
	cases := []struct {
		rows  []row
		limit int64
		want  int64
	}{
		{full, 3, 4},            // full page: last row's key
		{full[:2], 3, 0},        // short page: no next page
		{nil, 20, 0},            // empty page: no next page
		{[]row{{Seq: 5}}, 1, 5}, // page of one
	}
	for _, c := range cases {
		if got := NextAfter(c.rows, "seq", c.limit); got != c.want {
			t.Errorf("NextAfter(%v, seq, %d) = %d, want %d", c.rows, c.limit, got, c.want)
		}
	}
	if got := NextAfter([]row{{Seq: 1, AmountCents: 250}}, "amount_cents", 1); got != 250 {
		t.Errorf("snake_case column amount_cents did not match the sqlc field AmountCents: got %d", got)
	}
}

func TestNextAfterBrokenCallsAreBugs(t *testing.T) {
	for name, call := range map[string]func(){
		"unknown column": func() { NextAfter([]row{{Seq: 1}}, "nope", 1) },
		"over the limit": func() { NextAfter([]row{{Seq: 2}, {Seq: 1}}, "seq", 1) },
		"zero limit":     func() { NextAfter([]row{}, "seq", 0) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if _, ok := recover().(assert.Violation); !ok {
					t.Fatal("want an assertion violation")
				}
			}()
			call()
		})
	}
}
