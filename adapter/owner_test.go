package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	renameFixture      = "testdata/good/rename_event"
	adminRenameFixture = "testdata/good/admin_rename_event"
	myEventsFixture    = "testdata/good/my_events"
)

// appWithRoutes builds a module like the fixture app in a temporary
// directory, with its go.mod (requiring this checkout), internal/domain and
// schema.sql, but the given cmd/server/routes.go: for the cases that need
// other roles than the fixture app's.
func appWithRoutes(t *testing.T, routes string) string {
	t.Helper()
	root := t.TempDir()
	copyDir(t, "testdata/internal", filepath.Join(root, "internal"))
	write := func(rel, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", strings.Replace(mustRead(t, "testdata/go.mod"), "=> ../..", "=> "+mustAbs(t, ".."), 1))
	write("schema.sql", mustRead(t, "testdata/schema.sql"))
	write("cmd/server/routes.go", routes)
	return root
}

// inFixtureApp copies a fixture slice, with the edits addSlice applies, into
// the fixture app itself (testdata/tmp-*), so its schema.sql and cmd/server
// apply unchanged; it returns the slice dir.
func inFixtureApp(t *testing.T, fixture, name string, edits ...string) string {
	t.Helper()
	return addSlice(t, moduleTempDir(t), fixture, name, edits...)
}

// addSlice copies a fixture slice to <root>/features/<name> and applies the
// replacements (file, from, to, file, from, to, ...); it returns the slice dir.
func addSlice(t *testing.T, root, fixture, name string, edits ...string) string {
	t.Helper()
	dir := filepath.Join(root, "features", name)
	copyDir(t, fixture, dir)
	for i := 0; i+2 < len(edits); i += 3 {
		path := filepath.Join(dir, edits[i])
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), edits[i+1]) {
			t.Fatalf("%s no longer contains %q", path, edits[i+1])
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), edits[i+1], edits[i+2], 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestOwnershipEnglish (A4): every step on an owned table says whether it
// is limited to the caller's own rows, exactly.
func TestOwnershipEnglish(t *testing.T) {
	for dir, want := range map[string]string{
		renameFixture: "on each event whose `id` is the request's `event_id` and `organizer_id` is the signed-in user at that moment (query `RenameOwnEvent` in queries/rename_own_event.sql). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same event. " +
			"Ownership: only events you own (`organizer_id` is the signed-in user) can be changed by this step. If the query fails, stop with HTTP 500 Internal Server Error.\n",
		adminRenameFixture: "Ownership: this step is not limited to events you own (`organizer_id` need not be the signed-in user), because only role `admin` may call this action and cmd/server declares that it bypasses ownership. If the query fails",
		eventFixture:       "Call the stored row the new event. Ownership: the new event is yours (`organizer_id` is the signed-in user). If the query fails",
		myEventsFixture:    "(query `CountMyEvents` in queries/count_events.sql). Ownership: only events you own (`organizer_id` is the signed-in user) are read. If the query fails",
	} {
		got, err := Render(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q in:\n%s", dir, want, got)
		}
	}
	// A read that is not limited to the caller's rows is allowed, and says so.
	got, err := Render(inFixtureApp(t, myEventsFixture, "my_events",
		"queries/count_events.sql", "WHERE organizer_id = sqlc.arg(organizer_id)", "WHERE created_as = sqlc.arg(created_as)",
		"action.go", "a.q.CountMyEvents(ctx, in.User)", "a.q.CountMyEvents(ctx, in.Role)"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Ownership: this read is not limited to events you own (`organizer_id` is not compared with the signed-in user). If the query fails"; !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
	// Two roles that both bypass ownership.
	routes := strings.Replace(mustRead(t, "testdata/cmd/server/routes.go"), `.BypassOwnership("admin")`, `.BypassOwnership("admin", "finance")`, 1)
	got, err = Render(addSlice(t, appWithRoutes(t, routes), adminRenameFixture, "admin_rename_event", "action.go", `httpx.Roles("admin")`, `httpx.Roles("admin", "finance")`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "because only roles `admin` or `finance` may call this action and cmd/server declares that they bypass ownership."; !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
	// A table without an owner annotation (invoices): nothing is said about ownership.
	got, err = Render("testdata/good/create_invoice")
	if err != nil || strings.Contains(got, "Ownership") {
		t.Errorf("a table without an owner: %v\n%s", err, got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestOwnershipRefusals (A4): every write to an owned table that an action a
// role without the bypass may call does not limit to the signed-in user's
// rows is refused at file:line:col with its fix.
func TestOwnershipRefusals(t *testing.T) {
	const sql = "queries/rename_own_event.sql"
	const scoped = "WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);"
	const call = "db.RenameOwnEventParams{Title: in.Title, ID: in.EventID, OrganizerID: in.User}"
	const user = "User    int64  `json:\"user\" server:\"user\"`"
	const hint = "Table events is owned by organizer_id (schema.sql:"
	cases := map[string]struct {
		edits []string
		want  string
	}{
		"unscoped": {[]string{sql, scoped, "WHERE id = sqlc.arg(id);", "action.go", call, "db.RenameOwnEventParams{Title: in.Title, ID: in.EventID}"},
			"rename_event/action.go:51:2: refused: write to owned table events (query RenameOwnEvent) whose WHERE does not limit it to rows the signed-in user owns (organizer_id = the signed-in user) is not in the allowed pattern list (A4 ownership). " + hint},
		"owner only inside an OR group": {[]string{sql, scoped, "WHERE id = sqlc.arg(id) AND (organizer_id = sqlc.arg(organizer_id) OR title = '');"},
			"refused: write to owned table events (query RenameOwnEvent) whose WHERE does not limit it to rows the signed-in user owns"},
		"owner from the request": {[]string{"action.go", user, user + "\n\tMemberID int64  `json:\"member_id\"`", "action.go", "OrganizerID: in.User", "OrganizerID: in.MemberID"},
			"refused: write to owned table events (query RenameOwnEvent) whose owner column organizer_id is the request's `member_id`, which is not the signed-in user is not in the allowed pattern list (A4 ownership)"},
		"owner is a literal": {[]string{sql, scoped, "WHERE id = sqlc.arg(id) AND organizer_id = 7;", "action.go", call, "db.RenameOwnEventParams{Title: in.Title, ID: in.EventID}"},
			"rename_event/action.go:51:2: refused: write to owned table events (query RenameOwnEvent) whose owner column organizer_id is 7, which is not the signed-in user"},
		"public": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Public`},
			"refused: write to owned table events (query RenameOwnEvent) in a Public action is not in the allowed pattern list (A4 ownership)"},
		"no signed-in user": {[]string{"action.go", user, "Owner   int64  `json:\"owner\"`", "action.go", "OrganizerID: in.User", "OrganizerID: in.Owner"},
			"refused: write to owned table events (query RenameOwnEvent) in an action without the signed-in user is not in the allowed pattern list (A4 ownership)"},
		"not every role bypasses": {[]string{"action.go", `httpx.Roles("organizer")`, `httpx.Roles("organizer", "admin")`, sql, scoped, "WHERE id = sqlc.arg(id);", "action.go", call, "db.RenameOwnEventParams{Title: in.Title, ID: in.EventID}"},
			"refused: write to owned table events (query RenameOwnEvent) whose WHERE does not limit it"},
		"gives the row away": {[]string{sql, "SET title = sqlc.arg(title)", "SET title = sqlc.arg(title), organizer_id = sqlc.arg(new_owner)", "action.go", "OrganizerID: in.User}", "OrganizerID: in.User, NewOwner: in.EventID}"},
			"refused: change of the owner column organizer_id of owned table events (query RenameOwnEvent) is not in the allowed pattern list (A4 ownership)"},
		"user of another type": {[]string{"action.go", user, "User    string `json:\"user\" server:\"user\"`"},
			"rename_event/action.go:27:2: refused: signed-in user field User of type string for table events, whose owner column organizer_id is INTEGER (schema.sql:"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(inFixtureApp(t, renameFixture, "rename_event", tc.edits...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	// An insert that does not set the owner column.
	_, err := Render(inFixtureApp(t, eventFixture, "create_event",
		"queries/insert_event.sql", "INSERT INTO events (organizer_id, created_as, title, starts_at)\nVALUES (sqlc.arg(organizer_id), ", "INSERT INTO events (created_as, title, starts_at)\nVALUES (",
		"action.go", "OrganizerID: in.User,\n", ""))
	if want := "refused: insert into owned table events (query InsertEvent) that does not set its owner column organizer_id to the signed-in user is not in the allowed pattern list (A4 ownership)"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q in:\n%v", want, err)
	}
	// An admin-only action may do all of the above but give it to a Public action.
	if _, err := Render(inFixtureApp(t, renameFixture, "rename_event", "action.go", `httpx.Roles("organizer")`, `httpx.Roles("admin")`,
		sql, scoped, "WHERE id = sqlc.arg(id);", "action.go", call, "db.RenameOwnEventParams{Title: in.Title, ID: in.EventID}")); err != nil {
		t.Fatalf("admin-only: %v", err)
	}
}

// TestOwnerAnnotations (A4): the schema.sql annotation itself. Accepted on
// the comment lines right above CREATE TABLE or at the end of its line;
// everything else refused at schema.sql:L:C.
func TestOwnerAnnotations(t *testing.T) {
	const table = "CREATE TABLE IF NOT EXISTS docs (\n    id     INTEGER PRIMARY KEY,\n    author TEXT NOT NULL DEFAULT '--', -- not an owner\n    rank   INTEGER\n);\n"
	ok := map[string]string{
		"right above":            "-- owner: author\n" + table,
		"in a comment block":     "-- Documents.\n-- owner: author\n-- more words\n" + table,
		"integer":                "-- owner: rank\n" + table,
		"on the CREATE line":     strings.Replace(table, "docs (", "docs ( -- owner: author", 1),
		"quoted -- is not prose": "-- owner: author\n" + strings.Replace(table, "'--'", "'-- owner: x'", 1),
	}
	for name, schema := range ok {
		t.Run("ok/"+name, func(t *testing.T) {
			root := t.TempDir()
			os.WriteFile(filepath.Join(root, "schema.sql"), []byte(schema), 0o644)
			owners, errs, err := loadOwners(root)
			if err != nil || len(errs) > 0 || owners["docs"] == nil {
				t.Fatalf("owners %v, refused %v (%v)", owners, errs, err)
			}
			if want := map[bool]string{true: "int64", false: "string"}[name == "integer"]; owners["docs"].GoType != want {
				t.Fatalf("type %s, want %s", owners["docs"].GoType, want)
			}
		})
	}
	bad := map[string]struct{ schema, want string }{
		"unknown column": {"-- owner: writer\n" + table,
			"schema.sql:1:1: refused: owner column writer, which table docs does not declare (its columns: id, author, rank) is not in the allowed pattern list (A4 ownership). Declare a table's owner once"},
		"twice": {"-- owner: author\n-- owner: rank\n" + table,
			"schema.sql:2:1: refused: second owner annotation for table docs (the first is at schema.sql:1:1)"},
		"blank line": {"-- owner: author\n\n" + table,
			"schema.sql:1:1: refused: owner annotation \"-- owner: author\" that is not attached to a CREATE TABLE"},
		"on a column": {strings.Replace(table, "-- not an owner", "-- owner: author", 1),
			"schema.sql:3:40: refused: owner annotation \"-- owner: author\" that is not attached to a CREATE TABLE"},
		"no type": {"-- owner: rank\n" + strings.Replace(table, "rank   INTEGER", "rank", 1),
			"refused: owner column rank of type \"\", which is neither INTEGER nor TEXT"},
		"capitalised": {"-- Owner: author\n" + table, "schema.sql:1:1: refused: owner annotation \"-- Owner: author\" is not in the allowed pattern list (A4 ownership)"},
		"empty":       {"-- owner:\n" + table, "refused: owner annotation \"-- owner:\""},
		"at the end":  {table + "-- owner: author\n", "schema.sql:6:1: refused: owner annotation \"-- owner: author\" that is not attached to a CREATE TABLE"},
	}
	for name, tc := range bad {
		t.Run("bad/"+name, func(t *testing.T) {
			root := t.TempDir()
			os.WriteFile(filepath.Join(root, "schema.sql"), []byte(tc.schema), 0o644)
			owners, errs, err := loadOwners(root)
			if err != nil || !strings.Contains(errs.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v (%v)", tc.want, errs, err)
			}
			if name != "twice" && owners["docs"] != nil {
				t.Fatalf("a refused annotation owns docs: %+v", owners["docs"])
			}
		})
	}
}

// TestBypassOwnership (A4): the bypass is declared once, chained on the
// app's httpx.AppRoles call, with roles that call declares.
func TestBypassOwnership(t *testing.T) {
	routes := func(decl string) string {
		return "package main\n\nimport \"github.com/pierre10101/go-ai-bridge/runtime/httpx\"\n\n" + decl + "\n"
	}
	cases := map[string]struct{ decl, want string }{
		"chained":      {`var AppRoles = httpx.AppRoles("organizer", "admin").BypassOwnership("admin")`, ""},
		"not chained":  {"var AppRoles = httpx.AppRoles(\"organizer\", \"admin\")\nvar Admins = AppRoles.BypassOwnership(\"admin\")", `cmd/server/routes.go:6:14: refused: AppRoles.BypassOwnership that is not chained on the httpx.AppRoles call is not in the allowed pattern list (A4 ownership). Mark the roles that bypass ownership (A4) once`},
		"unknown role": {`var AppRoles = httpx.AppRoles("organizer", "admin").BypassOwnership("root")`, `cmd/server/routes.go:5:69: refused: ownership-bypass role "root" that httpx.AppRoles does not declare is not in the allowed pattern list (A4 ownership)`},
		"twice":        {`var AppRoles = httpx.AppRoles("organizer", "admin").BypassOwnership("admin", "admin")`, `refused: ownership-bypass role "admin" listed twice`},
		"empty":        {`var AppRoles = httpx.AppRoles("organizer", "admin").BypassOwnership()`, `refused: BypassOwnership without roles`},
		"not literal":  {"const admin = \"admin\"\n\nvar AppRoles = httpx.AppRoles(\"organizer\", \"admin\").BypassOwnership(admin)", `refused: ownership-bypass role admin that is not a string literal`},
		"second call":  {"var AppRoles = httpx.AppRoles(\"organizer\", \"admin\").BypassOwnership(\"admin\").BypassOwnership(\"organizer\")", `refused: httpx.AppRoles("organizer", "admin").BypassOwnership("admin").BypassOwnership that is not chained on the httpx.AppRoles call`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "cmd", "server"), 0o755)
			os.WriteFile(filepath.Join(root, "cmd", "server", "routes.go"), []byte(routes(tc.decl)), 0o644)
			roles, errs, err := loadAppRoles(root)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == "" && (len(errs) > 0 || !roles.passes["admin"] || roles.passes["organizer"]):
				t.Fatalf("refused %v, roles %+v", errs, roles)
			case tc.want != "" && !strings.Contains(errs.Error(), tc.want):
				t.Fatalf("want %q in:\n%v", tc.want, errs)
			}
		})
	}
}
