package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	addSectionFixture    = "testdata/good/add_section"
	renameSectionFixture = "testdata/good/rename_section"
	adminSectionFixture  = "testdata/good/admin_rename_section"
)

// pricesSchema is the fixture app's schema.sql with a grandchild table: a
// price belongs to a section, which belongs to an event (A5 chain).
func pricesSchema(t *testing.T) string {
	return mustRead(t, "testdata/schema.sql") + `
-- owner: section_id -> sections.event_id
CREATE TABLE IF NOT EXISTS prices (
    id         INTEGER PRIMARY KEY,
    section_id INTEGER NOT NULL REFERENCES sections (id),
    label      TEXT    NOT NULL,
    cents      INTEGER NOT NULL
);
`
}

// addFiles writes files (name, content, name, content, ...) into a new
// slice dir <root>/features/<name>.
func addFiles(t *testing.T, root, name string, files ...string) string {
	t.Helper()
	dir := filepath.Join(root, "features", name)
	for i := 0; i+1 < len(files); i += 2 {
		path := filepath.Join(dir, files[i])
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// priceAction is a slice on prices: one query call (sql, :execrows, with
// an S10 check unless read) taking SectionID, Label and the signed-in user.
func priceAction(name, roles, call string, read bool) string {
	route := `"POST /prices"`
	check := "\tif n != 1 {\n\t\treturn Output{}, F1\n\t}\n"
	fails := "var (\n\tF1 = failure.New(\"F1\", http.StatusNotFound, \"no such section of yours\")\n)\n"
	imports := "\t\"net/http\"\n\n"
	failImport := "\t\"github.com/pierre10101/go-ai-bridge/runtime/failure\"\n"
	if read {
		route, check, fails, imports, failImport = `"GET /prices/{id}"`, "", "", "\n", ""
	}
	in := "\tSectionID int64  `json:\"section_id\"`\n\tLabel     string `json:\"label\"`\n"
	if read {
		in = "\tSectionID int64  `json:\"section_id\" path:\"id\"`\n"
	}
	return "package " + name + `

import (
	"context"
` + imports + `	"example.com/fixtures/features/` + name + `/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
` + failImport + `	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = ` + route + `

var Roles = ` + roles + `

type Input struct {
` + in + "\tUser      int64  `json:\"user\" server:\"user\"`" + `
}

type Output struct {
	N int64 ` + "`json:\"n\"`" + `
}

` + fails + `
type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	n, err := a.q.` + call + `
	if err != nil {
		return Output{}, err
	}
` + check + `
	out := Output{N: n}
	assert.Post(out.N >= 0, "a count is never negative")
	return out, nil
}
`
}

const intentNone = "# Intent: X\n\n## Failure cases\nNone.\n"

// TestInheritedEnglish (A5): every step on a child table says whether it is
// limited to the caller's rows through the chain, exactly; a chain of two
// parents (prices -> sections -> events) is proved with nested subqueries.
func TestInheritedEnglish(t *testing.T) {
	for dir, want := range map[string]string{
		addSectionFixture: "3. Write: add one section to table `sections` with `event_id` = the event's `id`, `name` = the request's `name` and `capacity` = the request's `capacity`, only if there is an event whose `id` is the request's `event_id` and `organizer_id` is the signed-in user at that moment (query `AddSection` in queries/add_section.sql). " +
			"The condition is checked by the same statement that writes, never by an earlier read: if there is no such event, no section is added. Call the stored row the new section. " +
			"Ownership: the new section is added only to an event you own (`events.organizer_id` is the signed-in user); for any other event nothing is written. If the query fails, stop with HTTP 500 Internal Server Error.\n" +
			"4. If no section was added in step 3, stop with F3: HTTP 404 Not Found \"no such event of yours\".\n   Nothing was written in step 3, so there is nothing to roll back.\n",
		renameSectionFixture: "on each section only if `id` is the request's `section_id` and `event_id` is the `id` of an event whose `organizer_id` is the signed-in user at that moment (query `RenameOwnSection` in queries/rename_section.sql). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same section. " +
			"Ownership: only sections of events you own (`events.organizer_id` is the signed-in user) can be changed by this step. If the query fails",
		adminSectionFixture: "Ownership: this step is not limited to sections of events you own (`events.organizer_id` need not be the signed-in user), because only role `admin` may call this action and cmd/server declares that it bypasses ownership. If the query fails",
		myEventsFixture:     "2. Read: count the sections whose `event_id` is the `id` of an event whose `organizer_id` is the signed-in user (query `CountMySections` in queries/count_sections.sql). Ownership: only sections of events you own (`events.organizer_id` is the signed-in user) are read. If the query fails",
		summaryFixture:      "1. Read: count the sections whose `event_id` is the request's `event_id` (query `CountSections` in queries/count.sql). Ownership: this read is not limited to sections of events you own (`events.organizer_id` is not compared with the signed-in user). If the query fails",
	} {
		got, err := Render(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q in:\n%s", dir, want, got)
		}
	}

	// A chain: prices -> sections -> events.
	root := appWith(t, pricesSchema(t), "")
	const proof = "prices.section_id IN (SELECT sections.id FROM sections WHERE sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id)))"
	for _, tc := range []struct {
		name, sql, call, want string
		read                  bool
	}{
		{"add_price",
			"-- name: AddPrice :execrows\nINSERT INTO prices (section_id, label, cents)\nSELECT sections.id, sqlc.arg(label), 100 FROM sections\nWHERE sections.id = sqlc.arg(section_id) AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));\n",
			"AddPrice(ctx, db.AddPriceParams{Label: in.Label, SectionID: in.SectionID, OrganizerID: in.User})",
			"Write: add one price to table `prices` with `section_id` = the section's `id`, `label` = the request's `label` and `cents` = 100, only if there is a section whose `id` is the request's `section_id` and `event_id` is the `id` of an event whose `organizer_id` is the signed-in user at that moment (query `AddPrice` in queries/q.sql). The condition is checked by the same statement that writes, never by an earlier read: if there is no such section, no price is added. " +
				"Ownership: the new price is added only to a section of an event you own (`events.organizer_id` is the signed-in user); for any other section nothing is written.", false},
		{"rename_price",
			"-- name: RenamePrice :execrows\nUPDATE prices SET label = sqlc.arg(label)\nWHERE prices.id = sqlc.arg(section_id) AND " + proof + ";\n",
			"RenamePrice(ctx, db.RenamePriceParams{Label: in.Label, SectionID: in.SectionID, OrganizerID: in.User})",
			"on each price only if `id` is the request's `section_id` and `section_id` is the `id` of a section whose `event_id` is the `id` of an event whose `organizer_id` is the signed-in user at that moment (query `RenamePrice` in queries/q.sql). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same price. " +
				"Ownership: only prices of sections of events you own (`events.organizer_id` is the signed-in user) can be changed by this step.", false},
		{"count_prices",
			"-- name: CountPrices :one\nSELECT COUNT(*) FROM prices WHERE prices.section_id = sqlc.arg(section_id) AND " + proof + ";\n",
			"CountPrices(ctx, db.CountPricesParams{SectionID: in.SectionID, OrganizerID: in.User})",
			"Ownership: only prices of sections of events you own (`events.organizer_id` is the signed-in user) are read.", true},
	} {
		dir := addFiles(t, root, tc.name, "action.go", priceAction(tc.name, `httpx.Roles("organizer")`, tc.call, tc.read), "queries/q.sql", tc.sql)
		got, err := Render(dir)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in:\n%s", tc.name, tc.want, got)
		}
	}
	// One level of the chain is not enough: the section's event_id compared
	// with the signed-in user proves nothing.
	dir := addFiles(t, root, "short_proof", "action.go", priceAction("short_proof", `httpx.Roles("organizer")`, "RenamePrice(ctx, db.RenamePriceParams{Label: in.Label, SectionID: in.SectionID, OrganizerID: in.User})", false),
		"queries/q.sql", "-- name: RenamePrice :execrows\nUPDATE prices SET label = sqlc.arg(label)\nWHERE prices.id = sqlc.arg(section_id) AND prices.section_id IN (SELECT sections.id FROM sections WHERE sections.event_id = sqlc.arg(organizer_id));\n")
	_, err := Render(dir)
	if want := "q.sql:3:44: refused: condition on sections.event_id where the proof is sections.event_id IN (SELECT events.id FROM events ...) is not in the allowed pattern list (A5 inherited ownership). Table prices inherits its owner from sections (schema.sql:"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("want %q in:\n%v", want, err)
	}
}

// TestInheritedRefusals (A5): every write to a child table that an action a
// role without the bypass may call does not prove is the caller's, and
// every Q8 or Q9 that is not exactly the proof, is refused at file:line:col.
func TestInheritedRefusals(t *testing.T) {
	const sql = "queries/rename_section.sql"
	const proof = "  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));"
	const call = "db.RenameOwnSectionParams{Name: in.Name, ID: in.SectionID, OrganizerID: in.User}"
	const noUser = "db.RenameOwnSectionParams{Name: in.Name, ID: in.SectionID}"
	cases := map[string]struct {
		edits []string
		want  string
	}{
		"unproved": {[]string{sql, "\n" + proof, ";", "action.go", call, noUser},
			"rename_section/action.go:51:2: refused: write to table sections (query RenameOwnSection), which inherits its owner from events, whose WHERE does not prove the event is the signed-in user's is not in the allowed pattern list (A5 inherited ownership). Table sections inherits its owner from events through event_id (schema.sql:"},
		"proof inside an OR group": {[]string{sql, proof, "  AND (sections.name = '' OR sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id)));"},
			"refused: subquery inside an OR group is not in the allowed pattern list (query RenameOwnSection)"},
		"proof from the request": {[]string{"action.go", call, "db.RenameOwnSectionParams{Name: in.Name, ID: in.SectionID, OrganizerID: in.SectionID}"},
			"rename_section/action.go:51:116: refused: write to table sections (query RenameOwnSection), which inherits its owner from events, whose proof compares events.organizer_id with the request's `section_id`, which is not the signed-in user"},
		"proof with a literal": {[]string{sql, "sqlc.arg(organizer_id)", "7", "action.go", call, noUser},
			"whose proof compares events.organizer_id with 7, which is not the signed-in user"},
		"plain column": {[]string{sql, "WHERE sections.id", "WHERE id"},
			"rename_section.sql:5:7: refused: column id without its table in a query that reads two tables is not in the allowed pattern list (query RenameOwnSection). With a Q8 insert from a parent row or a Q9 subquery"},
		"another key": {[]string{sql, "SELECT events.id", "SELECT events.organizer_id"},
			"rename_section.sql:6:7: refused: subquery sections.event_id IN (SELECT events.organizer_id FROM events ...), but sections.event_id holds the key events.id is not in the allowed pattern list (A5 inherited ownership)"},
		"another table's column": {[]string{sql, "SELECT events.id FROM events", "SELECT sections.id FROM events"},
			"refused: subquery that selects sections.id FROM events is not in the allowed pattern list"},
		"a second condition": {[]string{sql, "events.organizer_id = sqlc.arg(organizer_id))", "events.organizer_id = sqlc.arg(organizer_id) AND events.title = 'x')"},
			"refused: subquery on events with 2 conditions is not in the allowed pattern list (A5 inherited ownership)"},
		"public": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Public`},
			"refused: write to table sections (query RenameOwnSection), which inherits its owner from events, in a Public action"},
		"not every role bypasses": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Roles("organizer", "admin")`, sql, "\n" + proof, ";", "action.go", call, noUser},
			"whose WHERE does not prove the event is the signed-in user's"},
		"moves the section": {[]string{sql, "SET name = sqlc.arg(name)", "SET name = sqlc.arg(name), event_id = 2"},
			"refused: change of the parent column event_id of table sections (query RenameOwnSection), which inherits its owner from events"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(inFixtureApp(t, renameSectionFixture, "rename_section", tc.edits...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	// An admin-only action may write any section.
	if _, err := Render(inFixtureApp(t, renameSectionFixture, "rename_section", "action.go", `httpx.Roles("organizer")`, `httpx.Roles("admin")`,
		sql, "\n"+proof, ";", "action.go", call, noUser)); err != nil {
		t.Fatalf("admin-only: %v", err)
	}

	// Q8, the insert from a parent row (fixture uses RETURNING id / :one).
	const add = "queries/add_section.sql"
	const sel = "SELECT events.id, sqlc.arg(name), sqlc.arg(capacity)"
	const where = "WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id)"
	const noRows = "\t\tif errors.Is(err, sql.ErrNoRows) {\n\t\t\treturn Output{}, F3\n\t\t}\n"
	q8 := map[string]struct {
		edits []string
		want  string
	}{
		"VALUES": {[]string{add, sel + "\nFROM events\n" + where + "\nRETURNING id;", "VALUES (sqlc.arg(event_id), sqlc.arg(name), sqlc.arg(capacity)) RETURNING id;",
			"action.go", "db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID, OrganizerID: in.User}", "db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID}"},
			"refused: insert into table sections (query AddSection), which inherits its owner from events, that does not prove the event is the signed-in user's is not in the allowed pattern list (A5 inherited ownership)"},
		"parent column from the request": {[]string{add, sel, "SELECT sqlc.arg(event_id), sqlc.arg(name), sqlc.arg(capacity)"},
			"add_section.sql:6:8: refused: value of the parent column event_id that is not events.id is not in the allowed pattern list (A5 inherited ownership)"},
		"not named by its key": {[]string{add, where, "WHERE events.organizer_id = sqlc.arg(organizer_id) AND events.title = sqlc.arg(event_id)"},
			"refused: condition on events.title in an insert from a parent row is not in the allowed pattern list (A5 inherited ownership)"},
		"no proof": {[]string{add, " AND events.organizer_id = sqlc.arg(organizer_id)", "", "action.go", ", OrganizerID: in.User}", "}"},
			"refused: insert from a row of events whose WHERE does not name it by events.id = <parameter> and prove it is the signed-in user's is not in the allowed pattern list (A5 inherited ownership)"},
		"as :execrows with RETURNING": {[]string{add, ":one", ":execrows"},
			"refused: query annotation :execrows on an insert from a parent row with RETURNING is not in the allowed pattern list (Q0 query annotation)"},
		"as :one without RETURNING": {[]string{add, "\nRETURNING id;", ";", add, ":one", ":execrows",
			"action.go", "sectionID, err := a.q.AddSection(ctx, db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID, OrganizerID: in.User})\n\tif err != nil {\n" + noRows + "\t\treturn Output{}, err\n\t}\n",
			"added, err := a.q.AddSection(ctx, db.AddSectionParams{Name: in.Name, Capacity: in.Capacity, EventID: in.EventID, OrganizerID: in.User})\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif added != 1 {\n\t\treturn Output{}, F3\n\t}\n",
			"action.go", "out := Output{SectionID: sectionID, EventID: in.EventID, Name: in.Name}", "out := Output{EventID: in.EventID, Name: in.Name}"},
			""}, // accepted: :execrows without RETURNING
		"unchecked": {[]string{"action.go", noRows, ""},
			"refused: insert from a parent row with RETURNING whose sql.ErrNoRows is not mapped to a failure is not in the allowed pattern list (S10 claim check)"},
		"into an owned table": {[]string{add, "INSERT INTO sections (event_id, name, capacity)", "INSERT INTO events (event_id, name, capacity)"},
			"add_section.sql:1:1: refused: insert from a row of events into table events, which does not inherit its owner is not in the allowed pattern list (A5 inherited ownership)"},
	}
	for name, tc := range q8 {
		t.Run("Q8/"+name, func(t *testing.T) {
			_, err := Render(inFixtureApp(t, addSectionFixture, "add_section", tc.edits...))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("accepted path refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	// A Q9 subquery on a table that does not inherit its owner.
	_, err := Render(inFixtureApp(t, renameFixture, "rename_event",
		"queries/rename_own_event.sql", "WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);", "WHERE events.id = sqlc.arg(id) AND events.organizer_id IN (SELECT sections.id FROM sections WHERE sections.event_id = sqlc.arg(organizer_id));"))
	if want := "rename_own_event.sql:5:36: refused: subquery on events.organizer_id, but table events does not inherit its owner is not in the allowed pattern list (A5 inherited ownership)"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q in:\n%v", want, err)
	}
}

// TestInheritedAnnotations (A5): an inherited annotation resolves through
// the chain to the owner column at its top and its type.
func TestInheritedAnnotations(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(`-- owner: prices_id -> prices.section_id
CREATE TABLE seats (id INTEGER PRIMARY KEY, prices_id INTEGER NOT NULL);
-- owner: section_id -> sections.event_id
CREATE TABLE prices (id INTEGER PRIMARY KEY, section_id INTEGER NOT NULL);
-- owner: event_id -> events.organizer
CREATE TABLE sections (id INTEGER PRIMARY KEY, event_id TEXT NOT NULL);
-- owner: organizer
CREATE TABLE events (id TEXT PRIMARY KEY, organizer TEXT NOT NULL);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	owners, errs, err := loadOwners(root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("refused %v (%v)", errs, err)
	}
	o := owners["seats"]
	if o == nil || o.root().Table != "events" || o.GoType != "string" || o.ParentKey != "id" || o.Parent.Table != "prices" || chainRows(o) != "seats of prices of sections of events" || rootCol(o) != "events.organizer" {
		t.Fatalf("seats: %+v", o)
	}
	if got := chainRow(o.Parent); got != "a price of a section of an event" {
		t.Fatalf("chainRow %q", got)
	}
}
