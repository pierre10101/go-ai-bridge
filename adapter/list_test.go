package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const confirmFixture = "testdata/good/confirm_many"

// mutate copies fixture into the fixture app, applies each from -> to (on
// action.go, or on the file named before ":" as in "queries/x.sql:...") and
// renders it.
func mutate(t *testing.T, fixture string, edits ...[3]string) (string, error) {
	t.Helper()
	dir := filepath.Join(moduleTempDir(t), filepath.Base(fixture))
	copyDir(t, fixture, dir)
	for _, e := range edits {
		file := e[0]
		if file == "" {
			file = "action.go"
		}
		path := filepath.Join(dir, file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), e[1]) {
			t.Fatalf("fixture %s no longer contains %q", file, e[1])
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), e[1], e[2], 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Render(dir)
}

// TestStrictClaimCheck (S10): only a guard whose entire condition is
// <n> != 1 checks a claim. The same test inside &&, || or ! does not count
// (claimed == 0 && claimed != 1 is claimed == 0: a claim that changed 2 rows
// passes it), but such guards may stay as extra guards.
func TestStrictClaimCheck(t *testing.T) {
	const check = "\tif claimed != 1 {\n\t\treturn Output{}, F1\n\t}\n"
	refused := map[string]struct{ to, want string }{
		"and":       {"\tif claimed == 0 && claimed != 1 {\n\t\treturn Output{}, F1\n\t}\n", "action.go:51:2: refused: claim whose changed-row count is checked only inside a compound condition (line 59) is not in the allowed pattern list (S10 claim check). The check is a guard of its own whose entire condition is claimed != 1"},
		"or":        {"\tif claimed != 1 || in.Session == 0 {\n\t\treturn Output{}, F1\n\t}\n", "refused: claim whose changed-row count is checked only inside a compound condition (line 59)"},
		"nested":    {"\tif in.Session > 0 && (claimed != 1 || in.SeatID == 0) {\n\t\treturn Output{}, F1\n\t}\n", "refused: claim whose changed-row count is checked only inside a compound condition (line 59)"},
		"negated":   {"\tif !(claimed == 1) {\n\t\treturn Output{}, F1\n\t}\n", "refused: claim whose changed-row count no guard checks is not in the allowed pattern list (S10 claim check)"},
		"two":       {"\tif claimed != 2 {\n\t\treturn Output{}, F1\n\t}\n", "refused: comparison claimed != 2 on a claim's changed-row count"},
		"in a post": {"\tassert.Pre(true, \"x\")\n", "refused: claim whose changed-row count no guard checks"},
	}
	for name, tc := range refused {
		t.Run("refused/"+name, func(t *testing.T) {
			_, err := mutate(t, "testdata/good/claim_example", [3]string{"", check, tc.to})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	accepted := map[string]struct{ to, want string }{
		"parenthesised": {"\tif (claimed != 1) {\n\t\treturn Output{}, F1\n\t}\n", "3. If (not exactly one seat was changed in step 2), stop with F1"},
		"extra compound guard": {"\tif claimed == 0 && in.Session == 7 {\n\t\treturn Output{}, F2\n\t}\n\tif claimed == 0 || claimed != 1 {\n\t\treturn Output{}, F1\n\t}\n" + check,
			"3. If no seat was changed in step 2 and the session from the cookie equals 7, stop with F2"},
	}
	for name, tc := range accepted {
		t.Run("ok/"+name, func(t *testing.T) {
			got, err := mutate(t, "testdata/good/claim_example", [3]string{"", check, tc.to})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
}

// TestMultiRowClaimRules (Q7, S11): a claim over <key> IN (sqlc.slice(ids))
// is checked by a guard whose entire condition is
// <n> != int64(len(in.<the list bound to ids>)); len appears nowhere else.
func TestMultiRowClaimRules(t *testing.T) {
	const check = "\tif confirmed != int64(len(in.TicketIDs)) {\n\t\treturn Output{}, F3\n\t}\n"
	const sqlFile = "queries/confirm_tickets.sql"
	const where = "WHERE held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now) AND id IN (sqlc.slice(ids));"
	refused := map[string]struct {
		edit [3]string
		want string
	}{
		"single-row check":  {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed != 1"}, "refused: comparison confirmed != 1 on a multi-row claim's changed-row count is not in the allowed pattern list (S11 multi-row claim check)"},
		"no check":          {[3]string{"", check, ""}, "refused: claim whose changed-row count no guard checks is not in the allowed pattern list (S11 multi-row claim check). After a claim over <key> IN (sqlc.slice(<name>)) (Q7), stop unless it changed one row per entry of the list: if confirmed != int64(len(in.TicketIDs))"},
		"reversed":          {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "int64(len(in.TicketIDs)) != confirmed"}, "refused: call to int64 is not in the allowed pattern list (expression)"},
		"equals":            {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed == int64(len(in.TicketIDs))"}, "refused: comparison confirmed == int64(len(in.TicketIDs)) on a claim's changed-row count is not in the allowed pattern list (S11 multi-row claim check)"},
		"no conversion":     {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed != len(in.TicketIDs)"}, "refused: comparison confirmed != len(in.TicketIDs) on a multi-row claim's changed-row count"},
		"compound":          {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed != int64(len(in.TicketIDs)) || held != 0"}, "refused: claim whose changed-row count is checked only inside a compound condition (line 65) is not in the allowed pattern list (S11 multi-row claim check)"},
		"len elsewhere":     {[3]string{"", "Output{Confirmed: confirmed}", "Output{Confirmed: int64(len(in.TicketIDs))}"}, "refused: call to int64 is not in the allowed pattern list (expression)"},
		"list as a value":   {[3]string{"", "\tif held != 0 {", "\tif in.TicketIDs == nil {\n\t\treturn Output{}, F2\n\t}\n\tif held != 0 {"}, "refused: list input in.TicketIDs used as a value is not in the allowed pattern list (D10 list input)"},
		"scalar for slice":  {[3]string{"", "Now: in.Now, Ids: in.TicketIDs}", "Now: in.Now, Ids: in.Now}"}, "refused: value in.Now for IN (sqlc.slice(ids)) that is not a list input is not in the allowed pattern list (Q7 IN list)"},
		"GET":               {[3]string{"", `"POST /tickets/confirm"`, `"GET /tickets/confirm"`}, "refused: list field TicketIDs in a GET action is not in the allowed pattern list (D10 list input)"},
		"no tag":            {[3]string{"", ` list:"1..20"`, ""}, "refused: list field TicketIDs without a list tag is not in the allowed pattern list (D10 list input)"},
		"empty allowed":     {[3]string{"", `list:"1..20"`, `list:"0..20"`}, `refused: list field TicketIDs whose list tag "0..20" must have 1 <= min <= max <= 100`},
		"slice first":       {[3]string{sqlFile, where, "WHERE id IN (sqlc.slice(ids)) AND held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now);"}, "confirm_tickets.sql:8:45: refused: parameter session after IN (sqlc.slice(ids)) is not in the allowed pattern list (query ConfirmTickets)"},
		"slice not on key":  {[3]string{sqlFile, "AND id IN (sqlc.slice(ids))", "AND held_by IN (sqlc.slice(ids))"}, "refused: claim over IN (sqlc.slice(ids)) on column held_by, which schema.sql does not declare as the single-column PRIMARY KEY of table tickets"},
		"two slices":        {[3]string{sqlFile, "held_by = sqlc.arg(session) AND expires_at", "held_by IN (sqlc.slice(holders)) AND expires_at"}, "refused: second IN (sqlc.slice(...))"},
		"slice in SET":      {[3]string{sqlFile, "held_by = ''", "held_by = sqlc.slice(ids)"}, "refused: sqlc.slice outside IN"},
		"literal IN list":   {[3]string{sqlFile, "IN (sqlc.slice(ids))", "IN (1, 2)"}, `refused: SQL "1" is not in the allowed pattern list (query ConfirmTickets). Expected (sqlc.slice(<name>)) after IN`},
		"single on a multi": {[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed == 1"}, "refused: comparison confirmed == 1 on a multi-row claim's changed-row count"},
	}
	for name, tc := range refused {
		t.Run("refused/"+name, func(t *testing.T) {
			_, err := mutate(t, confirmFixture, tc.edit)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	t.Run("refused/length of a single-row claim", func(t *testing.T) {
		_, err := mutate(t, confirmFixture,
			[3]string{sqlFile, " AND id IN (sqlc.slice(ids));", " AND id = sqlc.arg(id);"},
			[3]string{"", "Now: in.Now, Ids: in.TicketIDs}", "Now: in.Now, ID: 1}"})
		want := "refused: comparison confirmed != int64(len(in.TicketIDs)) on a single-row claim's changed-row count is not in the allowed pattern list (S11 multi-row claim check)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in:\n%v", want, err)
		}
	})
	t.Run("refused/length of another list", func(t *testing.T) {
		_, err := mutate(t, confirmFixture,
			[3]string{"", "\tSession   string", "\tOther     []int64 `json:\"other\" list:\"1..20\"`\n\tSession   string"},
			[3]string{"", "confirmed != int64(len(in.TicketIDs))", "confirmed != int64(len(in.Other))"})
		want := "refused: comparison with the length of in.Other, which is not the list of the claim in step 2 (in.TicketIDs)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in:\n%v", want, err)
		}
	})
	accepted := map[string]struct {
		edit [3]string
		want string
	}{
		"exact bounds":  {[3]string{"", `list:"1..20"`, `list:"3..3"`}, "- `ticket_ids`: a list of exactly 3 whole numbers with no duplicates.\n"},
		"text list":     {[3]string{"", "TicketIDs []int64", "TicketIDs []string"}, "- `ticket_ids`: a list of 1 to 20 text values with no duplicates.\n"},
		"nothing first": {[3]string{"", "\tif held != 0 {", "\tif confirmed == 0 && held != 0 {"}, "4. If no ticket was changed in step 2 and there is at least one ticket whose `held_by` is the session from the cookie and `expires_at` is no later than the current time and `id` is one of the request's `ticket_ids`, stop with F2: HTTP 410 Gone \"a hold has expired\".\n   Nothing was written in step 2, so there is nothing to roll back.\n"},
	}
	for name, tc := range accepted {
		t.Run("ok/"+name, func(t *testing.T) {
			got, err := mutate(t, confirmFixture, tc.edit)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
}

// TestMultiRowClaimWording pins the English of D10, Q7 and S11 and what each
// stopping step says about the multi-row claim's write.
func TestMultiRowClaimWording(t *testing.T) {
	got, err := Render(confirmFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- `ticket_ids`: a list of 1 to 20 whole numbers with no duplicates.\n",
		"; or a list has fewer or more entries than allowed above, has the same entry twice, or has an entry that is null; or the request sends a value that the server sets",
		"on each ticket only if `held_by` is the session from the cookie and `expires_at` is later than the current time and `id` is one of the request's `ticket_ids` at that moment",
		"3. Read: count the tickets whose `held_by` is the session from the cookie and `expires_at` is no later than the current time and `id` is one of the request's `ticket_ids` (query `CountStillHeld` in queries/tickets_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.\n   Any change made in step 2 is rolled back.\n",
		"5. If the number of tickets changed in step 2 is not the number of tickets in the request's `ticket_ids`, stop with F3: HTTP 409 Conflict \"a ticket is not held by this session\".\n   Any change made in step 2 is rolled back.\n",
		// after the S11 check: one row per entry changed, so a write happened
		"6. Read: count the tickets whose `sold_to` is the session from the cookie and `id` is one of the request's `ticket_ids` (query `CountSold` in queries/tickets_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.\n   The write in step 2 is rolled back.\n",
		"- F3 \"a ticket is not held by this session\": HTTP 409 Conflict; step 5, after a write that may have changed rows; any change it made is rolled back.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// TestPrimaryKeys: schema.sql gives each table's single-column key (Q7).
func TestPrimaryKeys(t *testing.T) {
	root := t.TempDir()
	schema := `-- comment (id) PRIMARY KEY
CREATE TABLE IF NOT EXISTS a (id INTEGER PRIMARY KEY, name TEXT);
create table b (
    code TEXT NOT NULL,
    n    INTEGER CHECK (n > 0),
    PRIMARY KEY (code)
);
CREATE TABLE c (x INTEGER, y INTEGER, PRIMARY KEY (x, y));
CREATE TABLE d (x INTEGER UNIQUE);
CREATE TABLE "e" (id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT, CONSTRAINT k UNIQUE (id));
`
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	keys, err := primaryKeys(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "id", "b": "code", "e": "id"}
	if len(keys) != len(want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	for k, v := range want {
		if keys[k] != v {
			t.Fatalf("keys %v, want %v", keys, want)
		}
	}
	if keys, err := primaryKeys(t.TempDir()); err != nil || len(keys) != 0 {
		t.Fatalf("no schema.sql: %v %v", keys, err)
	}
}

// TestINShapes: Q7 in reads (Q1, Q2) and claims (Q6); the list is the last
// parameter.
func TestINShapes(t *testing.T) {
	for name, src := range map[string]string{
		"count": "-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE held_by = ? AND id IN (sqlc.slice(ids));\n",
		"row":   "-- name: Q :one\nSELECT id, held_by FROM tickets WHERE id IN (sqlc.slice(ids));\n",
		"claim": "-- name: Q :execrows\nUPDATE tickets SET sold_to = sqlc.arg(s) WHERE held_by = sqlc.arg(s) AND id IN (sqlc.slice(ids));\n",
	} {
		qs, errs := loadOne(t, src)
		if len(errs) > 0 || qs["Q"].bad || qs["Q"].Slice != "ids" || qs["Q"].SliceCol != "id" {
			t.Fatalf("%s: %v %+v", name, errs, qs["Q"])
		}
	}
	for name, tc := range map[string]struct{ src, want string }{
		"param after":  {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE id IN (sqlc.slice(ids)) AND held_by = ?;\n", "q.sql:2:74: refused: parameter held_by after IN (sqlc.slice(ids))"},
		"bare ?":       {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE id IN (?);\n", "Expected (sqlc.slice(<name>)) after IN"},
		"subquery":     {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE id IN (SELECT id FROM tickets);\n", "Expected <table>.<key>: the subquery's column with its table"},
		"not in":       {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE id NOT IN (sqlc.slice(ids));\n", "refused: NOT is not in the allowed pattern list"},
		"in a group":   {"-- name: Q :execrows\nUPDATE tickets SET sold_to = ? WHERE held_by = ? AND (id IN (sqlc.slice(ids)) OR held_by = '');\n", "refused: IN inside an OR group"},
		"same name":    {"-- name: Q :one\nSELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(ids) AND id IN (sqlc.slice(ids));\n", "refused: sqlc.slice(ids) named like another parameter"},
		"slice insert": {"-- name: Q :one\nINSERT INTO tickets (id) VALUES (sqlc.slice(ids)) RETURNING id;\n", "refused: sqlc.slice outside IN"},
	} {
		_, errs := loadOne(t, tc.src)
		if len(errs) == 0 || !strings.Contains(errs.Error(), tc.want) {
			t.Fatalf("%s: want %q in:\n%v", name, tc.want, errs)
		}
	}
}
