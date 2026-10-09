package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	deleteSectionFixture = "testdata/good/delete_section"
	deleteEventFixture   = "testdata/good/delete_event"
	adminDeleteFixture   = "testdata/good/admin_delete_section"
)

// TestDeleteEnglish (Q10, A4, A5): a delete says which rows it removes, by
// the statement that deletes; what schema.sql does to the rows that
// reference them (ON DELETE CASCADE, RESTRICT); whether it is limited to
// the caller's rows; and its S10 guard says nothing was written.
func TestDeleteEnglish(t *testing.T) {
	const restrict = "While a section seat has `section_id` equal to the `id` of a section being deleted, schema.sql refuses the delete (ON DELETE RESTRICT): the query fails and nothing is deleted. "
	for dir, want := range map[string]string{
		deleteSectionFixture: "1. Delete: remove one section from table `sections` whose `id` is the request's `section_id` and `event_id` is the `id` of an event whose `organizer_id` is the signed-in user at that moment (query `DeleteOwnSection` in queries/delete_section.sql). " +
			"The condition is checked by the same statement that deletes, never by an earlier read: if there is no such section, nothing is deleted. " + restrict +
			"Ownership: only sections of events you own (`events.organizer_id` is the signed-in user) can be deleted by this step. If the query fails, stop with HTTP 500 Internal Server Error.\n" +
			"2. If not exactly one section was deleted in step 1, stop with F1: HTTP 404 Not Found \"no such section of yours\".\n   Nothing was written in step 1, so there is nothing to roll back.\n",
		deleteEventFixture: "1. Delete: remove one event from table `events` whose `id` is the request's `event_id` and `organizer_id` is the signed-in user at that moment (query `DeleteOwnEvent` in queries/delete_own_event.sql). " +
			"The condition is checked by the same statement that deletes, never by an earlier read: if there is no such event, nothing is deleted. " +
			"Each section whose `event_id` is the `id` of a deleted event is deleted with it (ON DELETE CASCADE in schema.sql). " + restrict +
			"Ownership: only events you own (`organizer_id` is the signed-in user) can be deleted by this step. If the query fails",
		adminDeleteFixture: restrict + "Ownership: this step is not limited to sections of events you own (`events.organizer_id` need not be the signed-in user), because only role `admin` may call this action and cmd/server declares that it bypasses ownership. If the query fails",
	} {
		en, err := Render(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if !strings.Contains(en, want) {
			t.Errorf("%s: want %q in:\n%s", dir, want, en)
		}
	}
	// A cascade is a write: the contract names every table it deletes from.
	if en, _ := Render(deleteEventFixture); !strings.Contains(en, "It reads table `events` and writes tables `events` and `sections`") {
		t.Errorf("delete_event: the cascade's table is not among the writes:\n%s", en)
	}
	// Path-only DELETE: Bind still requires a JSON body ({}), and the English
	// says so with InputRule and PathBodyRule (not GET's "no JSON body").
	en, err := Render(inFixtureApp(t, deleteEventFixture, "delete_event",
		"action.go", `const Route = "DELETE /events"`, `const Route = "DELETE /events/{id}"`,
		"action.go", "EventID int64 `json:\"event_id\"`", "EventID int64 `json:\"event_id\" path:\"id\"`"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The request takes this value from the path:\n- `event_id`, from the path (`{id}`): a whole number.\n",
		"The request body is one empty JSON object (`{}`).\n" +
			"Every field is required, also inside objects: a field that is left out, or is null, is answered with HTTP 400 below and the action does not run. " +
			"A field taken from the path is not sent in the body: a body that carries it is answered with HTTP 400 below and the action does not run.\n",
		"or the body carries a field that comes from the path. The action does not run.",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("path-only delete: want %q in:\n%s", want, en)
		}
	}
	if strings.Contains(en, "The request has no JSON body") {
		t.Errorf("path-only delete must not claim there is no JSON body:\n%s", en)
	}
	// A delete over a Q7 key list removes each listed row; S11 compares the
	// number deleted with the length of the list.
	en, err = Render(inFixtureApp(t, deleteSectionFixture, "delete_section",
		"queries/delete_section.sql", "WHERE sections.id = sqlc.arg(id)\n  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));",
		"WHERE sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id))\n  AND sections.id IN (sqlc.slice(ids));",
		"action.go", "SectionID int64 `json:\"section_id\"`", "SectionIDs []int64 `json:\"section_ids\" list:\"1..10\"`",
		"action.go", "SectionID int64 `json:\"section_id\"`", "Deleted int64 `json:\"deleted\"`",
		"action.go", "db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: in.User}", "db.DeleteOwnSectionParams{OrganizerID: in.User, Ids: in.SectionIDs}",
		"action.go", "deleted != 1", "deleted != int64(len(in.SectionIDs))",
		"action.go", "Output{SectionID: in.SectionID}", "Output{Deleted: deleted}",
		"action.go", "out.SectionID == in.SectionID", "out.Deleted == deleted"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"1. Delete: remove each section from table `sections` whose `event_id` is the `id` of an event whose `organizer_id` is the signed-in user and `id` is one of the request's `section_ids` at that moment (query `DeleteOwnSection` in queries/delete_section.sql). The condition is checked by the same statement that deletes, never by an earlier read: a section that does not match is not deleted.",
		"2. If the number of sections deleted in step 1 is not the number of sections in the request's `section_ids`, stop with F1",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("want %q in:\n%s", want, en)
		}
	}
}

// TestDeleteRefusals (Q10, A4, A5): a delete that an action a role without
// the bypass may call does not limit to the caller's rows, or that is not
// checked, is refused at file:line:col, as an update would be.
func TestDeleteRefusals(t *testing.T) {
	const sql = "queries/delete_section.sql"
	const proof = "\n  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));"
	const call = "db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: in.User}"
	cases := map[string]struct {
		edits []string
		want  string
	}{
		"proof from the request": {[]string{"action.go", call, "db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: in.SectionID}"},
			"delete_section/action.go:43:101: refused: delete from table sections (query DeleteOwnSection), which inherits its owner from events, whose proof compares events.organizer_id with the request's `section_id`, which is not the signed-in user is not in the allowed pattern list (A5 inherited ownership)"},
		"without the signed-in user": {[]string{"action.go", "\tUser      int64 `json:\"user\" server:\"user\"`\n", "", "action.go", call, "db.DeleteOwnSectionParams{ID: in.SectionID, OrganizerID: 7}"},
			"refused: delete from table sections (query DeleteOwnSection), which inherits its owner from events, in an action without the signed-in user"},
		"not every role bypasses": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Roles("organizer", "admin")`, sql, proof, ";", "action.go", call, "in.SectionID"},
			"refused: delete from table sections (query DeleteOwnSection), which inherits its owner from events, whose WHERE does not prove the event is the signed-in user's"},
		"proof inside parentheses": {[]string{sql, proof, "\n  AND (sections.name = '' OR sections.event_id = 1);"},
			"delete_section.sql:6:7: refused: parenthesised condition in a delete is not in the allowed pattern list (query DeleteOwnSection)"},
		"no count check": {[]string{"action.go", "\tif deleted != 1 {\n\t\treturn Output{}, F1\n\t}\n", ""},
			"refused: claim whose changed-row count no guard checks"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(inFixtureApp(t, deleteSectionFixture, "delete_section", tc.edits...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	// An admin-only action may delete any section.
	if _, err := Render(inFixtureApp(t, deleteSectionFixture, "delete_section", "action.go", `httpx.Roles("organizer")`, `httpx.Roles("admin")`,
		sql, proof, ";", "action.go", call, "in.SectionID", "action.go", "\tUser      int64 `json:\"user\" server:\"user\"`\n", "")); err != nil {
		t.Fatalf("admin delete refused: %v", err)
	}

	const ecall = "db.DeleteOwnEventParams{ID: in.EventID, OrganizerID: in.User}"
	for name, tc := range map[string]struct {
		edits []string
		want  string
	}{
		"owner from the request": {[]string{"action.go", ecall, "db.DeleteOwnEventParams{ID: in.EventID, OrganizerID: in.EventID}"},
			"delete_event/action.go:42:95: refused: delete from owned table events (query DeleteOwnEvent) whose owner column organizer_id is the request's `event_id`, which is not the signed-in user is not in the allowed pattern list (A4 ownership)"},
		"owner literal": {[]string{"queries/delete_own_event.sql", "organizer_id = sqlc.arg(organizer_id)", "organizer_id = 7", "action.go", ecall, "in.EventID"},
			"refused: delete from owned table events (query DeleteOwnEvent) whose owner column organizer_id is 7, which is not the signed-in user"},
		"public": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Public`},
			"refused: delete from owned table events (query DeleteOwnEvent) in a Public action"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Render(inFixtureApp(t, deleteEventFixture, "delete_event", tc.edits...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}

	// A cascade writes the tables it deletes from: reading one of them
	// earlier in the action is check-then-write (W1).
	root := moduleTempDir(t)
	dir := addSlice(t, root, deleteEventFixture, "delete_event",
		"action.go", "\tdeleted, err := a.q.DeleteOwnEvent", "\tsections, err := a.q.CountSections(ctx, in.EventID)\n\tif err != nil {\n\t\treturn Output{}, err\n\t}\n\tif sections > 9 {\n\t\treturn Output{}, F1\n\t}\n\tdeleted, err := a.q.DeleteOwnEvent")
	addFiles(t, root, "delete_event", "queries/count.sql", "-- name: CountSections :one\nSELECT COUNT(*) FROM sections WHERE event_id = sqlc.arg(event_id);\n")
	if _, err := Render(dir); err == nil || !strings.Contains(err.Error(), "refused: write to table sections after reading it in step 1 (check-then-write)") {
		t.Fatalf("want W1 on the cascade's table, got %v", err)
	}
}

// deleteChecks loads one query file against a schema, as -check does, and
// returns the Q10 refusals and the parsed queries.
func deleteChecks(t *testing.T, schema, sql string) (string, map[string]*SQLQuery) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "queries", "q.sql"), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, errs, err := LoadQueries(filepath.Join(root, "queries"))
	if err != nil {
		t.Fatal(err)
	}
	keyErrs, err := checkClaimKeys(root, qs)
	if err != nil {
		t.Fatal(err)
	}
	delErrs, err := checkDeletes(root, qs)
	if err != nil {
		t.Fatal(err)
	}
	return Refusals(append(append(errs, keyErrs...), delErrs...)).Error(), qs
}

// TestDeleteSchema (Q10): the key and the foreign keys that reference a
// deleted row are checked against schema.sql. Every form of foreign key is
// read; a cascade is followed to the tables it reaches; anything but ON
// DELETE CASCADE or RESTRICT is refused.
func TestDeleteSchema(t *testing.T) {
	const parents = `CREATE TABLE a (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE tags (name TEXT NOT NULL);
`
	del := func(table string) string {
		return "-- name: D :execrows\nDELETE FROM " + table + " WHERE id = sqlc.arg(id);\n"
	}
	for name, tc := range map[string]struct {
		schema, sql, want string
	}{
		"no key":          {parents, "-- name: D :execrows\nDELETE FROM tags WHERE name = sqlc.arg(name);\n", "q.sql:1:1: refused: delete from table tags (query D), which schema.sql does not give a single-column PRIMARY KEY is not in the allowed pattern list (Q10 delete)"},
		"not the key":     {parents, "-- name: D :execrows\nDELETE FROM a WHERE name = sqlc.arg(name);\n", "refused: delete from table a (query D) whose WHERE does not name its rows by the key id"},
		"list not on key": {parents, "-- name: D :execrows\nDELETE FROM a WHERE name IN (sqlc.slice(names));\n", "refused: claim over IN (sqlc.slice(names)) on column name, which schema.sql does not declare as the single-column PRIMARY KEY of table a"},
		"no on delete": {parents + "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER NOT NULL REFERENCES a (id));\n", del("a"),
			"refused: delete from table a (query D) while b.a_id references a without ON DELETE (schema.sql:3:63) is not in the allowed pattern list (Q10 delete)"},
		"set null": {parents + "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a ON DELETE SET NULL);\n", del("a"),
			"refused: delete from table a (query D) while b.a_id references a with ON DELETE SET NULL"},
		"no action, table constraint": {parents + "CREATE TABLE b (\n  id INTEGER PRIMARY KEY,\n  a_id INTEGER NOT NULL,\n  CONSTRAINT fk_a FOREIGN KEY (a_id) REFERENCES a (id) ON DELETE NO ACTION\n);\n", del("a"),
			"refused: delete from table a (query D) while b.a_id references a with ON DELETE NO ACTION (schema.sql:6:38)"},
		"through a cascade": {parents + "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER NOT NULL REFERENCES a (id) ON DELETE CASCADE);\nCREATE TABLE c (id INTEGER PRIMARY KEY, b_id INTEGER NOT NULL REFERENCES b (id));\n", del("a"),
			"refused: delete from table a (query D), which deletes rows of b with it (ON DELETE CASCADE), while c.b_id references b without ON DELETE"},
	} {
		t.Run(name, func(t *testing.T) {
			if got, _ := deleteChecks(t, tc.schema, tc.sql); !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
	// Accepted: a cascade two levels deep, a self-reference, a RESTRICT and
	// a table constraint; the effects in order and every cascaded table
	// among the writes.
	schema := parents + `CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER NOT NULL REFERENCES a (id) ON DELETE CASCADE ON UPDATE CASCADE);
CREATE TABLE c (id INTEGER PRIMARY KEY, b_id INTEGER NOT NULL, parent_id INTEGER REFERENCES c ON DELETE CASCADE, FOREIGN KEY (b_id) REFERENCES b (id) ON DELETE CASCADE);
CREATE TABLE d (id INTEGER PRIMARY KEY, c_id INTEGER NOT NULL REFERENCES c (id) ON DELETE RESTRICT);
`
	got, qs := deleteChecks(t, schema, del("a"))
	if got != "" {
		t.Fatalf("refused: %s", got)
	}
	var effects []string
	for _, e := range qs["D"].OnDelete {
		effects = append(effects, e.Table+"."+e.Col+" -> "+e.RefTable+"."+e.RefCol+" "+e.OnDelete)
	}
	if want := "b.a_id -> a.id CASCADE; c.b_id -> b.id CASCADE; c.parent_id -> c.id CASCADE; d.c_id -> c.id RESTRICT"; strings.Join(effects, "; ") != want {
		t.Errorf("effects %q, want %q", strings.Join(effects, "; "), want)
	}
	if w := strings.Join(qs["D"].Writes, ","); w != "a,b,c" {
		t.Errorf("writes %q", w)
	}
}
