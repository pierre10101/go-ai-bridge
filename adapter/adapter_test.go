package adapter

import (
	"flag"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden .en and .err files")

// goodDirs lists every fixture slice under testdata/good. Each is a full
// slice of the fixture app (testdata/go.mod): action.go, queries/, intent.md,
// checks/ and its golden <slice>.en. scripts/smoke-app.sh builds them in a new
// app and runs their checks.
func goodDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob("testdata/good/*/action.go")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no good fixtures found: %v", err)
	}
	for i := range dirs {
		dirs[i] = filepath.Dir(dirs[i])
	}
	return dirs
}

// TestGoldenEnglish: adapter output must match the committed .en file byte
// for byte. RULEBOOK.md quotes these files.
func TestGoldenEnglish(t *testing.T) {
	for _, dir := range goodDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			got, err := Render(dir)
			if err != nil {
				t.Fatalf("unrenderable:\n%v", err)
			}
			golden := GoldenPath(dir)
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("%s is stale (%s)\nrun: go test ./adapter -run TestGoldenEnglish -update, then review the diff", golden, firstDiff(string(want), got))
			}
		})
	}
}

// TestRulebook: intent F-IDs, check coverage, dead SQL and route binding, per slice.
func TestRulebook(t *testing.T) {
	for _, dir := range goodDirs(t) {
		if err := Check(dir); err != nil {
			t.Errorf("%s:\n%v", dir, err)
		}
	}
}

// TestRenderIsDeterministic: same input, same bytes.
func TestRenderIsDeterministic(t *testing.T) {
	for _, dir := range goodDirs(t) {
		a, _ := Render(dir)
		b, _ := Render(dir)
		if a != b {
			t.Fatalf("%s renders differently on two runs", dir)
		}
	}
}

// TestRefusesDeliberateRuleBreaks: each testdata/bad/<case>/action.go must be
// refused with exactly the messages in testdata/bad/<case>/want.err.
func TestRefusesDeliberateRuleBreaks(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/bad/*")
	if len(dirs) == 0 {
		t.Fatal("no bad cases")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			_, err := Render(dir)
			if err == nil {
				t.Fatal("adapter rendered a rule break; it must refuse")
			}
			got := err.Error() + "\n"
			wantPath := filepath.Join(dir, "want.err")
			if *update {
				if err := os.WriteFile(wantPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("refusal changed\n--- want\n%s--- got\n%s", want, got)
			}
		})
	}
}

// TestClaimRules: mutations of the claim fixture around T1, Q6, S10 and W1.
func TestClaimRules(t *testing.T) {
	const fixture = "testdata/good/claim_example"
	src, err := os.ReadFile(filepath.Join(fixture, "action.go"))
	if err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile(filepath.Join(fixture, "queries", "claim_seat.sql"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ goFrom, goTo, sqlFrom, sqlTo, want string }{
		"no claim check": {goFrom: "\tif claimed != 1 {\n\t\treturn Output{}, F1\n\t}\n", goTo: "",
			want: "refused: claim whose changed-row count no guard checks is not in the allowed pattern list (S10 claim check)"},
		"greater than zero": {goFrom: "claimed != 1", goTo: "claimed > 0",
			want: "refused: comparison claimed > 0 on a claim's changed-row count"},
		"clock field from the caller": {goFrom: "`json:\"now\" clock:\"now\"`", goTo: "`json:\"now\" clock:\"utc\"`",
			want: "refused: clock field Now that is not int64 tagged clock:\"now\""},
		"clock on output": {goFrom: "Now    int64 `json:\"now\"`", goTo: "Now    int64 `json:\"now\" clock:\"now\"`",
			want: "refused: clock tag on Now outside Input"},
		"execrows needed": {sqlFrom: ":execrows", sqlTo: ":one",
			want: "refused: query annotation :one on a claim update is not in the allowed pattern list (Q0 query annotation)"},
		"no where": {sqlFrom: "\nWHERE id = sqlc.arg(id) AND (held_by = 0 OR expires_at <= sqlc.arg(now))", sqlTo: "",
			want: "refused: end of statement is not in the allowed pattern list (query ClaimSeat). Expected WHERE after SET"},
		"no id": {sqlFrom: "id = sqlc.arg(id) AND ", sqlTo: "",
			want: "refused: claim update with no <col> = <parameter> condition"},
		"returning": {sqlFrom: "sqlc.arg(now));", sqlTo: "sqlc.arg(now)) RETURNING id;",
			want: "refused: RETURNING on an UPDATE is not in the allowed pattern list (query ClaimSeat)"},
		"or outside a group": {sqlFrom: "(held_by = 0 OR expires_at <= sqlc.arg(now))", sqlTo: "held_by = 0 OR expires_at <= sqlc.arg(now)",
			want: "refused: OR in WHERE is not in the allowed pattern list (query ClaimSeat)"},
		"subquery": {sqlFrom: "(held_by = 0 OR expires_at <= sqlc.arg(now))", sqlTo: "(SELECT 1)",
			want: "refused: subquery is not in the allowed pattern list (query ClaimSeat)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(moduleTempDir(t), "claim_example")
			copyDir(t, fixture, dir)
			for _, m := range []struct{ file, src, from, to string }{
				{"action.go", string(src), tc.goFrom, tc.goTo},
				{filepath.Join("queries", "claim_seat.sql"), string(sql), tc.sqlFrom, tc.sqlTo},
			} {
				if m.from == "" {
					continue
				}
				if !strings.Contains(m.src, m.from) {
					t.Fatalf("fixture %s no longer contains %q", m.file, m.from)
				}
				if err := os.WriteFile(filepath.Join(dir, m.file), []byte(strings.Replace(m.src, m.from, m.to, 1)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Render(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	t.Run("write then read is fine (W1 is read-then-write only)", func(t *testing.T) {
		dir := filepath.Join(moduleTempDir(t), "claim_example")
		copyDir(t, fixture, dir)
		read := "-- name: SeatHolder :one\nSELECT held_by FROM seats WHERE id = ?;\n"
		if err := os.WriteFile(filepath.Join(dir, "queries", "seat_holder.sql"), []byte(read), 0o644); err != nil {
			t.Fatal(err)
		}
		mutated := strings.Replace(string(src), "\tif claimed != 1 {", "\tholder, err := a.q.SeatHolder(ctx, in.SeatID)\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif claimed == 0 && holder.HeldBy == in.Session {\n\t\treturn Output{}, F2\n\t}\n\tif claimed != 1 {", 1)
		if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(mutated), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Render(dir)
		if err != nil {
			t.Fatalf("refused a diagnostic read after the claim: %v", err)
		}
		if !strings.Contains(got, "If no seat was changed in step 2 and the found seat's `held_by` equals the session from the cookie, stop with F2") {
			t.Fatalf("English:\n%s", got)
		}
	})
}

// TestRefusalsInHandle covers single constructs, one at a time.
func TestRefusalsInHandle(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"defer":        {"defer func() {}()", `action.go:LINE:2: refused: defer statement is not in the allowed pattern list (Handle body)`},
		"goroutine":    {"go func() {}()", `refused: go statement (goroutine)`},
		"switch":       {"switch in.Currency {\n\t}", `refused: switch statement`},
		"if/else":      {"if in.AmountCents <= 0 {\n\t\treturn Output{}, F1\n\t} else {\n\t}", `refused: if/else statement`},
		"mutation":     {"x := in.AmountCents\n\tx = 3", `refused: reassignment (=)`},
		"arithmetic":   {"x := in.AmountCents + 1", `refused: arithmetic operator +`},
		"index":        {"x := in.Currency[0]", `refused: index expression`},
		"other call":   {"x := len(in.Currency)", `refused: call to len`},
		"late pre":     {"x := in.AmountCents\n\tassert.Pre(x > 0, \"late\")", `refused: precondition after other statements`},
		"unchecked q":  {"c, err := a.q.CustomerExists(ctx, in.CustomerID)", `refused: query whose error is not checked`},
		"pre reads in": {"x := in.AmountCents", `action.go:33:13: refused: precondition that reads the request (in) is not in the allowed pattern list (S1 precondition)`},
		"no phrase":    {"x := domain.Undocumented(in.Currency)", `refused: domain.Undocumented, which internal/domain does not declare as a function`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := moduleTempDir(t)
			src := strings.Replace(miniAction, "BODY", tc.body, 1)
			if name == "pre reads in" {
				src = strings.Replace(src, `assert.Pre(a.q != nil, "queries are wired")`, `assert.Pre(in.AmountCents > 0, "positive")`, 1)
			}
			if strings.Contains(tc.body, "domain.") {
				src = strings.Replace(src, `"github.com/pierre10101/go-ai-bridge/runtime/assert"`, "\"github.com/pierre10101/go-ai-bridge/runtime/assert\"\n\t\"example.com/fixtures/internal/domain\"", 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Render(dir)
			if err == nil {
				t.Fatal("rendered; want refusal")
			}
			want := strings.Replace(tc.want, "LINE", "34", 1)
			if strings.Contains(tc.body, "domain.") {
				want = strings.Replace(want, "34", "35", 1)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q in:\n%v", want, err)
			}
		})
	}
}

const miniAction = `package mini

import (
	"context"
	"net/http"

	"example.com/fixtures/features/mini/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)

const Route = "POST /mini"

type Input struct {
	CustomerID  int64  ` + "`json:\"customer_id\"`" + `
	AmountCents int64  ` + "`json:\"amount_cents\"`" + `
	Currency    string ` + "`json:\"currency\"`" + `
}

type Output struct {
	OK bool ` + "`json:\"ok\"`" + `
}

var F1 = failure.New("F1", http.StatusBadRequest, "bad")

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	assert.Pre(a.q != nil, "queries are wired")
	BODY
	out := Output{OK: true}
	assert.Post(out.OK, "ok")
	return out, nil
}
`

// moduleTempDir is a scratch slice inside the fixture app (testdata/go.mod),
// so the adapter reads its internal/domain from the module root.
func moduleTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("testdata", "tmp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestSQLShapes: only Q1-Q3 render; everything else is refused with file:line:col.
func TestSQLShapes(t *testing.T) {
	ok := map[string]string{
		"count":        "SELECT COUNT(*) FROM customers WHERE id = ?;",
		"count two":    "SELECT COUNT(*) FROM customers WHERE id = ? AND name = sqlc.arg(name)",
		"row":          "SELECT id, name FROM customers WHERE id = ?;",
		"insert":       "INSERT INTO invoices (seq, customer_id, currency) VALUES ((SELECT COALESCE(MAX(seq), 0) + 1 FROM invoices), ?, 'ZAR') RETURNING id, seq;",
		"insert named": "insert into invoices (customer_id) values (sqlc.arg(customer_id)) returning id",
	}
	for name, body := range ok {
		t.Run("ok/"+name, func(t *testing.T) {
			qs, errs := loadOne(t, "-- name: Q :one\n"+body+"\n")
			if len(errs) > 0 || qs["Q"] == nil || qs["Q"].bad {
				t.Fatalf("refused a recognised shape: %v", errs)
			}
		})
	}
	t.Run("ok/page", func(t *testing.T) {
		qs, errs := loadOne(t, "-- name: Q :many\nSELECT seq, amount_cents FROM invoices WHERE customer_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?;\n")
		if len(errs) > 0 || qs["Q"] == nil || qs["Q"].bad || qs["Q"].Shape != "page" {
			t.Fatalf("refused Q5 page: %v shape=%v", errs, qs["Q"])
		}
	})
	t.Run("ok/page bare", func(t *testing.T) {
		qs, errs := loadOne(t, "-- name: Q :many\nSELECT id, title FROM books WHERE id < ? ORDER BY id DESC LIMIT ?;\n")
		if len(errs) > 0 || qs["Q"] == nil || qs["Q"].bad || qs["Q"].Shape != "page" {
			t.Fatalf("refused Q5 page with only cursor: %v shape=%v", errs, qs["Q"])
		}
		if len(qs["Q"].Where) != 0 || qs["Q"].CursorCol != "id" {
			t.Fatalf("bare page: where=%v cursor=%q", qs["Q"].Where, qs["Q"].CursorCol)
		}
	})
	t.Run("ok/page join", func(t *testing.T) {
		qs, errs := loadOne(t, "-- name: Q :many\nSELECT sections.id, sections.name, events.title\nFROM sections\nJOIN events ON sections.event_id = events.id\nWHERE sections.event_id = ? AND sections.id < ?\nORDER BY sections.id DESC\nLIMIT ?;\n")
		if len(errs) > 0 || qs["Q"] == nil || qs["Q"].bad || qs["Q"].Shape != "page" {
			t.Fatalf("refused Q5 page with JOIN: %v shape=%v", errs, qs["Q"])
		}
		if qs["Q"].JoinTable != "events" || qs["Q"].JoinFK != "event_id" || qs["Q"].JoinPK != "id" {
			t.Fatalf("join: %+v", qs["Q"])
		}
	})
	refused := map[string]struct{ body, want string }{
		"left join":        {"SELECT sections.id FROM sections LEFT JOIN events ON sections.event_id = events.id WHERE sections.id < ? ORDER BY sections.id DESC LIMIT ?;", "LEFT JOIN"},
		"insert or ignore": {"INSERT OR IGNORE INTO invoices (seq) VALUES (?) RETURNING id;", "q.sql:2:8: refused: INSERT OR IGNORE is not in the allowed pattern list (query Q). Expected INTO after INSERT"},
		"replace":          {"REPLACE INTO invoices (seq) VALUES (?) RETURNING id;", "q.sql:2:1: refused: REPLACE statement"},
		"update returning": {"UPDATE customers SET name = ? WHERE id = ? RETURNING id;", "q.sql:2:44: refused: RETURNING on an UPDATE"},
		"update as :one":   {"UPDATE customers SET name = ? WHERE id = ?;", "q.sql:1:1: refused: query annotation :one on a claim update"},
		"update no where":  {"UPDATE customers SET name = ?;", "Expected WHERE after SET"},
		"update bare ? x2": {"UPDATE seats SET expires_at = ? WHERE id = ? AND expires_at <= ?;", "q.sql:2:64: refused: second bare ? for column expires_at"},
		"delete returning": {"DELETE FROM customers WHERE id = ? RETURNING id;", "q.sql:2:36: refused: RETURNING on a DELETE is not in the allowed pattern list (query Q). Expected end of query (a Q10 delete gives back only the number of rows it removed"},
		"delete as :one":   {"DELETE FROM customers WHERE id = ?;", "q.sql:1:1: refused: query annotation :one on a delete is not in the allowed pattern list (Q0 query annotation)"},
		"delete no where":  {"DELETE FROM customers;", "q.sql:2:22: refused: DELETE without WHERE is not in the allowed pattern list (query Q). A DELETE without WHERE removes every row of customers"},
		"delete or":        {"DELETE FROM customers WHERE id = ? OR name = ?;", "q.sql:2:36: refused: OR in WHERE"},
		"delete group":     {"DELETE FROM customers WHERE (id = ? OR name = ?);", "q.sql:2:29: refused: parenthesised condition in a delete"},
		"delete range":     {"DELETE FROM customers WHERE id > ?;", "q.sql:2:29: refused: comparison id > sqlc.arg(id) in a delete"},
		"delete literal":   {"DELETE FROM customers WHERE name = 'x';", "q.sql:1:1: refused: delete with no <key> = <parameter> condition"},
		"delete limit":     {"DELETE FROM customers WHERE id = ? LIMIT 1;", "q.sql:2:36: refused: LIMIT"},
		"delete subquery":  {"DELETE FROM customers WHERE id = (SELECT 1);", "q.sql:2:34: refused: subquery"},
		"with":             {"WITH c AS (SELECT 1) SELECT COUNT(*) FROM c WHERE id = ?;", "q.sql:2:1: refused: WITH (common table expression)"},
		"join":             {"SELECT seq FROM invoices JOIN customers ON customers.id = invoices.customer_id WHERE seq = ?;", "JOIN in a non-page SELECT"},
		"or":               {"SELECT COUNT(*) FROM customers WHERE id = ? OR name = ?;", "q.sql:2:45: refused: OR in WHERE"},
		"select star":      {"SELECT * FROM customers WHERE id = ?;", "q.sql:2:8: refused: SELECT *"},
		"no where":         {"SELECT COUNT(*) FROM customers;", "q.sql:2:31: refused: end of statement is not in the allowed pattern list (query Q). Expected WHERE after FROM customers"},
		"upsert":           {"INSERT INTO customers (id, name) VALUES (?, ?) ON CONFLICT DO NOTHING RETURNING id;", "q.sql:2:48: refused: ON CONFLICT (upsert)"},
		"two statements":   {"SELECT COUNT(*) FROM customers WHERE id = ?; DELETE FROM customers WHERE id = ?;", "q.sql:2:46: refused: second statement in one query"},
		"subquery":         {"SELECT COUNT(*) FROM customers WHERE id = (SELECT 1);", "q.sql:2:43: refused: subquery"},
		"like":             {"SELECT COUNT(*) FROM customers WHERE name LIKE ?;", "q.sql:2:43: refused: LIKE"},
		"limit":            {"SELECT id FROM customers WHERE id = ? LIMIT 1;", "q.sql:2:39: refused: LIMIT"},
		"insert no return": {"INSERT INTO invoices (seq) VALUES (?);", "q.sql:2:38: refused: end of statement is not in the allowed pattern list (query Q). Expected RETURNING"},
		"next other table": {"INSERT INTO invoices (seq) VALUES ((SELECT COALESCE(MAX(seq), 0) + 1 FROM customers)) RETURNING id;", "refused: next-number subquery over customers"},
		"count column":     {"SELECT COUNT(id) FROM customers WHERE id = ?;", "q.sql:2:14: refused: SQL \"id\""},
	}
	for name, tc := range refused {
		t.Run("refused/"+name, func(t *testing.T) {
			_, errs := loadOne(t, "-- name: Q :one\n"+tc.body+"\n")
			if len(errs) == 0 || !strings.Contains(errs.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, errs)
			}
		})
	}
	t.Run("ok/claim", func(t *testing.T) {
		qs, errs := loadOne(t, "-- name: Q :execrows\nUPDATE seats SET held_by = ?, expires_at = sqlc.arg(now) + 600 WHERE id = ? AND (held_by = 0 OR expires_at <= sqlc.arg(now));\n")
		if len(errs) > 0 || qs["Q"] == nil || qs["Q"].bad || qs["Q"].Shape != "claim" {
			t.Fatalf("refused Q6 claim: %v", errs)
		}
		if got := strings.Join(qs["Q"].Params, ","); got != "held_by,now,id" {
			t.Fatalf("params %s", got)
		}
	})
	t.Run("refused/execrows on a select", func(t *testing.T) {
		_, errs := loadOne(t, "-- name: Q :execrows\nSELECT COUNT(*) FROM customers WHERE id = ?;\n")
		if !strings.Contains(errs.Error(), "query annotation :execrows on a non-claim shape") {
			t.Fatal(errs)
		}
	})
	t.Run("refused/many without page", func(t *testing.T) {
		_, errs := loadOne(t, "-- name: Q :many\nSELECT id FROM customers WHERE id = ?;\n")
		if !strings.Contains(errs.Error(), "query annotation :many on a non-page shape") {
			t.Fatal(errs)
		}
	})
	t.Run("refused/offset", func(t *testing.T) {
		_, errs := loadOne(t, "-- name: Q :many\nSELECT seq FROM invoices WHERE customer_id = ? AND seq < ? ORDER BY seq DESC LIMIT ? OFFSET ?;\n")
		if !strings.Contains(errs.Error(), "OFFSET") {
			t.Fatal(errs)
		}
	})
	t.Run("refused/no annotation", func(t *testing.T) {
		_, errs := loadOne(t, "SELECT COUNT(*) FROM customers WHERE id = ?;\n")
		if !strings.Contains(errs.Error(), "q.sql:1:1: refused: SQL before any -- name: annotation") {
			t.Fatal(errs)
		}
	})
}

func loadOne(t *testing.T, src string) (map[string]*SQLQuery, Refusals) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "q.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, errs, err := LoadQueries(dir)
	if err != nil {
		t.Fatal(err)
	}
	return qs, errs
}

// TestEnglishHasNoGoNames: the golden English names things by their JSON
// fields, tables and domain phrases, never by Go identifiers: no Go operator,
// no package selector, and no local variable of Handle in backticks.
func TestEnglishHasNoGoNames(t *testing.T) {
	goSyntax := regexp.MustCompile("domain\\.|db\\.|\\bnil\\b|:=|!=|`[a-z]+\\.[A-Z]")
	for _, dir := range goodDirs(t) {
		got, err := Render(dir)
		if err != nil {
			t.Fatal(err)
		}
		forbidden := handleLocals(t, filepath.Join(dir, "action.go"))
		for i, line := range strings.Split(got, "\n") {
			if m := goSyntax.FindString(line); m != "" {
				t.Errorf("%s line %d leaks Go (%q): %s", dir, i+1, m, line)
			}
			for _, name := range forbidden {
				// A local used as a subject ("`customers` equals 0") or named as
				// a result ("called `row`"). Tables may share a local's name.
				leak := regexp.MustCompile("`" + name + "` (equals|is|does|has)|called `" + name + "`")
				if leak.MatchString(line) {
					t.Errorf("%s line %d names Go local %q: %s", dir, i+1, name, line)
				}
			}
		}
	}
}

// handleLocals lists a, ctx, in and every name Handle defines with :=.
func handleLocals(t *testing.T, path string) []string {
	t.Helper()
	file, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"a", "ctx", "in", "err"}
	ast.Inspect(file, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			for _, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name != "err" {
					names = append(names, id.Name)
				}
			}
		}
		return true
	})
	return names
}

// TestCoverageNeedsReference: TestF1_ must reference <pkg>.F1, not just be named for it.
func TestCoverageNeedsReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package checks

import (
	"testing"

	"example.com/fixtures/features/demo"
)

func TestF1_NamedOnly(t *testing.T) { _ = demo.F2 }

func TestF2_Real(t *testing.T) { _ = demo.F2 }
`
	if err := os.WriteFile(filepath.Join(dir, "checks", "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	errs := checkCoverage(dir, "demo", map[string]bool{"F1": true, "F2": true})
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	got := strings.Join(all, "\n")
	for _, want := range []string{"TestF1_NamedOnly is named for F1 but never references demo.F1", "no check covers F1"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "F2") {
		t.Errorf("F2 is covered; got:\n%s", got)
	}
}

// TestListSliceRules: mutations of the list slice that must be refused (S9
// next cursor tied to the page query; no writes in a GET).
func TestListSliceRules(t *testing.T) {
	src, err := os.ReadFile("testdata/good/list_customer_invoices/action.go")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ from, to, want string }{
		"cursor column": {`page.NextAfter(rows, "seq", in.Limit)`, `page.NextAfter(rows, "amount_cents", in.Limit)`,
			"refused: page.NextAfter key that is not the page query's cursor column"},
		"cursor limit": {`page.NextAfter(rows, "seq", in.Limit)`, `page.NextAfter(rows, "seq", in.After)`,
			"refused: page.NextAfter limit the request's `after`, which is not the page query's LIMIT (the request's `limit`)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(string(src), tc.from) {
				t.Fatalf("slice no longer contains %q", tc.from)
			}
			dir := filepath.Join(moduleTempDir(t), "list_customer_invoices")
			copyDir(t, "testdata/good/list_customer_invoices", dir)
			mutated := strings.Replace(string(src), tc.from, tc.to, 1)
			if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Render(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	t.Run("write in GET", func(t *testing.T) {
		dir := filepath.Join(moduleTempDir(t), "list_customer_invoices")
		copyDir(t, "testdata/good/list_customer_invoices", dir)
		insert := "-- name: AddInvoice :one\nINSERT INTO invoices (customer_id) VALUES (?) RETURNING seq;\n"
		if err := os.WriteFile(filepath.Join(dir, "queries", "add.sql"), []byte(insert), 0o644); err != nil {
			t.Fatal(err)
		}
		mutated := strings.Replace(string(src), "\tcustomers, err := a.q.CustomerExists(ctx, in.CustomerID)",
			"\tadded, err := a.q.AddInvoice(ctx, in.CustomerID)\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\t_ = added\n\tcustomers, err := a.q.CustomerExists(ctx, in.CustomerID)", 1)
		if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(mutated), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Render(dir)
		if err == nil || !strings.Contains(err.Error(), "refused: write query AddInvoice in a GET action") {
			t.Fatalf("got %v", err)
		}
	})
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
