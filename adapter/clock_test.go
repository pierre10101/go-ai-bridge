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
// "col <= now - 600" (10 minutes or more ago) for "within the last 10
// minutes". The fixture itself compares with no offset (expires_at <= now);
// the claim checks (testdata/good/claim_example/checks), run by
// scripts/smoke-app.sh, prove that boundary against SQLite: a hold that
// expires one second after now still blocks, one that expires now does not.
func TestClockComparisonWording(t *testing.T) {
	const fixture = "testdata/good/claim_example"
	sql, err := os.ReadFile(filepath.Join(fixture, "queries", "claim_seat.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const cond = "expires_at <= sqlc.arg(now)"
	if !strings.Contains(string(sql), cond) {
		t.Fatalf("fixture no longer contains %q", cond)
	}
	for _, tc := range []struct{ op, want string }{
		{"<=", "`expires_at` is 10 minutes or more before the current time"},
		{"<", "`expires_at` is more than 10 minutes before the current time"},
		{">", "`expires_at` is later than 10 minutes before the current time"},
		{">=", "`expires_at` is no earlier than 10 minutes before the current time"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			dir := filepath.Join(moduleTempDir(t), "claim_example")
			copyDir(t, fixture, dir)
			mutated := strings.Replace(string(sql), cond, "expires_at "+tc.op+" sqlc.arg(now) - 600", 1)
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
		{"<=", "`expires_at` is no later than the current time"},
		{"<", "`expires_at` is earlier than the current time"},
		{">", "`expires_at` is later than the current time"},
		{">=", "`expires_at` is no earlier than the current time"},
		{"=", "`expires_at` is exactly the current time"},
		{"<>", "`expires_at` is not exactly the current time"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			dir := filepath.Join(moduleTempDir(t), "claim_example")
			copyDir(t, fixture, dir)
			mutated := strings.Replace(string(sql), "expires_at <= sqlc.arg(now)", "expires_at "+tc.op+" sqlc.arg(now)", 1)
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
	mutated := strings.Replace(string(src), "out.Now == in.Now,", "out.Now <= in.Now,", 1)
	if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Render(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "the answer's `now` is no later than the current time"; !strings.Contains(got, want) {
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
	mutated := strings.Replace(string(sql), "expires_at <= sqlc.arg(now)", "expires_at >= sqlc.arg(now) + 3600", 1)
	if err := os.WriteFile(filepath.Join(dir, "queries", "claim_seat.sql"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Render(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "`expires_at` is 1 hour or more after the current time"; !strings.Contains(got, want) {
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

// TestClockComparisonInReads: a Q1 count or Q2 one-row read may compare a
// column with the server-set current time (T1), with or without an offset,
// and says it in the Q6 boundary words; a guard on such a count says "there
// is at least one <row> whose ...", so the words stay exactly the same. The
// value must be the action's clock input itself.
func TestClockComparisonInReads(t *testing.T) {
	const cond = "expires_at <= sqlc.arg(now)"
	for _, tc := range []struct{ sql, want string }{
		{"expires_at <= sqlc.arg(now)", "`expires_at` is no later than the current time"},
		{"expires_at < sqlc.arg(now)", "`expires_at` is earlier than the current time"},
		{"expires_at > sqlc.arg(now)", "`expires_at` is later than the current time"},
		{"expires_at >= sqlc.arg(now)", "`expires_at` is no earlier than the current time"},
		{"expires_at <> sqlc.arg(now)", "`expires_at` is not exactly the current time"},
		{"expires_at != sqlc.arg(now)", "`expires_at` is not exactly the current time"},
		{"expires_at <= sqlc.arg(now) - 600", "`expires_at` is 10 minutes or more before the current time"},
		{"expires_at > sqlc.arg(now) + 3600", "`expires_at` is more than 1 hour after the current time"},
		{"expires_at = sqlc.arg(now) - 60", "`expires_at` is exactly 1 minute before the current time"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			got, err := mutate(t, confirmFixture, [3]string{"queries/tickets_after.sql", cond, tc.sql})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"3. Read: count the tickets whose `held_by` is the session from the cookie and " + tc.want + " and `id` is one of the request's `ticket_ids` (query `CountStillHeld`",
				"4. If there is at least one ticket whose `held_by` is the session from the cookie and " + tc.want + " and `id` is one of the request's `ticket_ids`, stop with F2",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("want %q in:\n%s", want, got)
				}
			}
			for _, vague := range []string{"is at most the current time", "is less than the current time", "equal to the current time"} {
				if strings.Contains(got, vague) {
					t.Fatalf("%q in:\n%s", vague, got)
				}
			}
		})
	}
	t.Run("no clock comparison: has", func(t *testing.T) {
		got, err := mutate(t, confirmFixture,
			[3]string{"queries/tickets_after.sql", "AND " + cond + " ", ""},
			[3]string{"", "db.CountStillHeldParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs}", "db.CountStillHeldParams{Session: in.Session, Ids: in.TicketIDs}"})
		if err != nil {
			t.Fatal(err)
		}
		if want := "4. If at least one ticket has `held_by` equal to the session from the cookie and `id` equal to one of the request's `ticket_ids`, stop with F2"; !strings.Contains(got, want) {
			t.Fatalf("want %q in:\n%s", want, got)
		}
	})
	t.Run("Q2 one row", func(t *testing.T) {
		got, err := mutate(t, releaseFixture,
			[3]string{"queries/seat_after.sql", "SELECT held_by, expires_at FROM seats WHERE id = ?;", "SELECT held_by, expires_at FROM seats WHERE id = sqlc.arg(id) AND expires_at > sqlc.arg(now);"},
			[3]string{"", "Session int64 `json:\"session\" server:\"session\"`", "Session int64 `json:\"session\" server:\"session\"`\n\tNow     int64 `json:\"now\" clock:\"now\"`"},
			[3]string{"", "a.q.SeatHolder(ctx, in.SeatID)", "a.q.SeatHolder(ctx, db.SeatHolderParams{ID: in.SeatID, Now: in.Now})"})
		if err != nil {
			t.Fatal(err)
		}
		if want := "Read: find a seat whose `id` is the request's `seat_id` and `expires_at` is later than the current time (query `SeatHolder`"; !strings.Contains(got, want) {
			t.Fatalf("want %q in:\n%s", want, got)
		}
	})
	for name, tc := range map[string]struct {
		edits [][3]string
		want  string
	}{
		"request field": {[][3]string{
			{"", "Session   string  `json:\"session\" server:\"session\"`", "Session   string  `json:\"session\" server:\"session\"`\n\tUntil     int64   `json:\"until\"`"},
			{"", "Now: in.Now, Ids: in.TicketIDs})\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif held", "Now: in.Until, Ids: in.TicketIDs})\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif held"},
		}, "refused: comparison expires_at <= sqlc.arg(now) in query CountStillHeld, whose value in.Until is not the server-set current time is not in the allowed pattern list (Q1 count)"},
		"literal": {[][3]string{
			{"", "Now: in.Now, Ids: in.TicketIDs})\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif held", "Now: 1800000000, Ids: in.TicketIDs})\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif held"},
		}, "whose value 1800000000 is not the server-set current time"},
		"let": {[][3]string{
			{"", "\theld, err := a.q.CountStillHeld(ctx, db.CountStillHeldParams{Session: in.Session, Now: in.Now,", "\tnow := in.Now\n\theld, err := a.q.CountStillHeld(ctx, db.CountStillHeldParams{Session: in.Session, Now: now,"},
		}, "whose value now is not the server-set current time"},
		"other clock-named parameter": {[][3]string{
			{"queries/tickets_after.sql", cond, "expires_at <= sqlc.arg(until)"},
			{"", "db.CountStillHeldParams{Session: in.Session, Now: in.Now,", "db.CountStillHeldParams{Session: in.Session, Until: in.Session,"},
		}, "whose value in.Session is not the server-set current time"},
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			_, err := mutate(t, confirmFixture, tc.edits...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// TestClockInReadShapes: the SQL side. A comparison with the current time in
// a read keeps every parameter in statement order, so the IN list stays last
// (Q7); a keyset page is still a page, and may not compare with the clock.
func TestClockInReadShapes(t *testing.T) {
	for name, tc := range map[string]struct{ src, params string }{
		"count": {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(now) AND id IN (sqlc.slice(seat_ids));\n", "session,now,seat_ids"},
		"row":   {"-- name: Q :one\nSELECT id FROM tickets WHERE expires_at > sqlc.arg(now) - 600 AND held_by = ?;\n", "now,held_by"},
		"page":  {"-- name: Q :many\nSELECT seq FROM invoices WHERE customer_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?;\n", "customer_id,seq,limit"},
	} {
		qs, errs := loadOne(t, tc.src)
		if len(errs) > 0 || qs["Q"].bad {
			t.Fatalf("%s: %v", name, errs)
		}
		if got := strings.Join(qs["Q"].Params, ","); got != tc.params {
			t.Fatalf("%s: params %s, want %s", name, got, tc.params)
		}
	}
	for name, tc := range map[string]struct{ src, want string }{
		"after IN":       {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE id IN (sqlc.slice(ids)) AND expires_at <= sqlc.arg(now);\n", "refused: parameter now after IN (sqlc.slice(ids))"},
		"on the left":    {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE sqlc.arg(now) >= expires_at;\n", "refused: parameter on the left of a condition"},
		"bare ? left":    {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE ? >= expires_at;\n", "refused: parameter on the left of a condition"},
		"literal":        {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE held_by = ? AND expires_at <= 5;\n", "refused: comparison expires_at <= 5 in a read"},
		"text":           {"-- name: Q :one\nSELECT id FROM tickets WHERE held_by <> 'x';\n", "refused: comparison held_by <> 'x' in a read"},
		"literal offset": {"-- name: Q :one\nSELECT id FROM tickets WHERE expires_at = 5 + 1;\n", "Expected end of query"},
		"page":           {"-- name: Q :many\nSELECT id FROM tickets WHERE held_by = ? AND expires_at <= sqlc.arg(now) AND id < ? ORDER BY id DESC LIMIT 5;\n", "refused: comparison expires_at <= sqlc.arg(now) in a keyset page"},
		"page offset":    {"-- name: Q :many\nSELECT id FROM tickets WHERE held_by = sqlc.arg(now) + 5 AND id < ? ORDER BY id DESC LIMIT 5;\n", "refused: comparison held_by = sqlc.arg(now) + 5 in a keyset page"},
		"or":             {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE held_by = ? OR expires_at <= sqlc.arg(now);\n", "refused: OR in WHERE"},
		"between":        {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE expires_at BETWEEN ? AND ?;\n", "refused: BETWEEN"},
	} {
		_, errs := loadOne(t, tc.src)
		if len(errs) == 0 || !strings.Contains(errs.Error(), tc.want) {
			t.Fatalf("%s: want %q in %v", name, tc.want, errs)
		}
	}
}
