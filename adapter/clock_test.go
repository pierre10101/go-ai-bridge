package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClockComparisonWording pins the exact English of a Q6 condition that
// compares a column with the current time minus an offset, for all four
// operators, through the whole pipeline (SQL parser, claim rendering). Each
// phrase names the boundary in so many words, so no reader can take
// "held_at <= now - 600" (held 10 minutes or more ago) for "within the last
// 10 minutes". The claim checks (testdata/good/claim_example/checks), run by
// scripts/smoke-app.sh, prove the <= boundary against SQLite: 599 seconds
// later the hold still blocks, 600 seconds later it does not.
func TestClockComparisonWording(t *testing.T) {
	const fixture = "testdata/good/claim_example"
	sql, err := os.ReadFile(filepath.Join(fixture, "queries", "claim_seat.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const cond = "held_at <= sqlc.arg(now) - 600"
	if !strings.Contains(string(sql), cond) {
		t.Fatalf("fixture no longer contains %q", cond)
	}
	for _, tc := range []struct{ op, want string }{
		{"<=", "`held_at` is 10 minutes or more before the current time"},
		{"<", "`held_at` is more than 10 minutes before the current time"},
		{">", "`held_at` is later than 10 minutes before the current time"},
		{">=", "`held_at` is no earlier than 10 minutes before the current time"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			dir := filepath.Join(moduleTempDir(t), "claim_example")
			copyDir(t, fixture, dir)
			mutated := strings.Replace(string(sql), cond, "held_at "+tc.op+" sqlc.arg(now) - 600", 1)
			if err := os.WriteFile(filepath.Join(dir, "queries", "claim_seat.sql"), []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Render(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := "on each seat whose `id` is the request's `seat_id` and (`held_by` is 0 or " + tc.want + ") at that moment"
			if !strings.Contains(got, want) {
				t.Fatalf("want %q in:\n%s", want, got)
			}
		})
	}
}

// TestClockComparisonTable pins every operator on both sides of the current
// time, and how the offset is said.
func TestClockComparisonTable(t *testing.T) {
	for _, tc := range []struct {
		op, sign string
		seconds  int
		want     string
	}{
		{"<=", "-", 600, "`held_at` is 10 minutes or more before the current time"},
		{"<", "-", 600, "`held_at` is more than 10 minutes before the current time"},
		{">", "-", 600, "`held_at` is later than 10 minutes before the current time"},
		{">=", "-", 600, "`held_at` is no earlier than 10 minutes before the current time"},
		{"=", "-", 600, "`held_at` is exactly 10 minutes before the current time"},
		{"<>", "-", 600, "`held_at` is not exactly 10 minutes before the current time"},
		{">=", "+", 86400, "`held_at` is 1 day or more after the current time"},
		{">", "+", 7200, "`held_at` is more than 2 hours after the current time"},
		{"<", "+", 90, "`held_at` is earlier than 90 seconds after the current time"},
		{"<=", "+", 60, "`held_at` is no later than 1 minute after the current time"},
		{"=", "+", 1, "`held_at` is exactly 1 second after the current time"},
		{"<>", "+", 3600, "`held_at` is not exactly 1 hour after the current time"},
		// No offset: the same boundary words against the current time itself.
		{"<=", "", 0, "`expires_at` is no later than the current time"},
		{"<", "", 0, "`expires_at` is earlier than the current time"},
		{">", "", 0, "`expires_at` is later than the current time"},
		{">=", "", 0, "`expires_at` is no earlier than the current time"},
		{"=", "", 0, "`expires_at` is exactly the current time"},
		{"<>", "", 0, "`expires_at` is not exactly the current time"},
	} {
		var got string
		if tc.sign == "" {
			got = clockNow("`expires_at`", tc.op)
		} else {
			got = clockComparison("held_at", tc.op, tc.sign, tc.seconds)
		}
		if got != tc.want {
			t.Errorf("%s now %s %d:\n got  %s\n want %s", tc.op, tc.sign, tc.seconds, got, tc.want)
		}
	}
}

// TestClockComparisonWithoutOffset: a Q6 condition against the current time
// itself (no offset) says its boundary like the offset rows, through the
// whole pipeline; never "is at most the current time".
func TestClockComparisonWithoutOffset(t *testing.T) {
	const fixture = "testdata/good/claim_example"
	sql, err := os.ReadFile(filepath.Join(fixture, "queries", "claim_seat.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ op, want string }{
		{"<=", "`held_at` is no later than the current time"},
		{"<", "`held_at` is earlier than the current time"},
		{">", "`held_at` is later than the current time"},
		{">=", "`held_at` is no earlier than the current time"},
		{"=", "`held_at` is exactly the current time"},
		{"<>", "`held_at` is not exactly the current time"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			dir := filepath.Join(moduleTempDir(t), "claim_example")
			copyDir(t, fixture, dir)
			mutated := strings.Replace(string(sql), "held_at <= sqlc.arg(now) - 600", "held_at "+tc.op+" sqlc.arg(now)", 1)
			if err := os.WriteFile(filepath.Join(dir, "queries", "claim_seat.sql"), []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Render(dir)
			if err != nil {
				t.Fatal(err)
			}
			if want := "(`held_by` is 0 or " + tc.want + ") at that moment"; !strings.Contains(got, want) {
				t.Fatalf("want %q in:\n%s", want, got)
			}
			for _, vague := range []string{"is at most the current time", "is at least the current time", "is less than the current time", "is greater than the current time"} {
				if strings.Contains(got, vague) {
					t.Fatalf("%q in:\n%s", vague, got)
				}
			}
		})
	}
	// A Go comparison (E3) with the current time on the right says the same.
	src, err := os.ReadFile(filepath.Join(fixture, "action.go"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(moduleTempDir(t), "claim_example")
	copyDir(t, fixture, dir)
	mutated := strings.Replace(string(src), "out.HeldBy == in.PersonID,", "out.HeldBy == in.PersonID && out.HeldAt <= in.Now,", 1)
	if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Render(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "the answer's `held_at` is no later than the current time"; !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// TestClockComparisonGoesThroughSQL: a "+" offset and a non-clock parameter
// render as expected from a real Q6 query.
func TestClockComparisonGoesThroughSQL(t *testing.T) {
	const fixture = "testdata/good/claim_example"
	sql, err := os.ReadFile(filepath.Join(fixture, "queries", "claim_seat.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(moduleTempDir(t), "claim_example")
	copyDir(t, fixture, dir)
	mutated := strings.Replace(string(sql), "held_at <= sqlc.arg(now) - 600", "held_at >= sqlc.arg(now) + 3600", 1)
	if err := os.WriteFile(filepath.Join(dir, "queries", "claim_seat.sql"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Render(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "`held_at` is 1 hour or more after the current time"; !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// TestClockArgInDomainCall: a domain function called with the current time
// says its comparisons against it in the Q6 boundary words.
func TestClockArgInDomainCall(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"{e} is at most {now}", "{e} is no later than {now}"},
		{"{e} is less than {now}", "{e} is earlier than {now}"},
		{"{e} is greater than {now}", "{e} is later than {now}"},
		{"{e} is at least {now}", "{e} is no earlier than {now}"},
		{"{e} equals {now}", "{e} is exactly {now}"},
		{"{e} does not equal {now}", "{e} is not exactly {now}"},
		{"{s} equals 0 and ({h} equals 0 or {e} is at most {now})", "{s} equals 0 and ({h} equals 0 or {e} is no later than {now})"},
		{"{now} is at most {e}", "{now} is at most {e}"},       // the clock on the left: unchanged
		{"{e} is at most {nowish}", "{e} is at most {nowish}"}, // another parameter
	} {
		if got := clockArg(tc.in, "{now}"); got != tc.want {
			t.Errorf("clockArg(%q):\n got  %s\n want %s", tc.in, got, tc.want)
		}
	}
}
