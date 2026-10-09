package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const eventFixture = "testdata/good/create_event"

// mutateEvent copies the create_event fixture into the fixture app (so
// cmd/server's httpx.AppRoles applies), applies from -> to on action.go and
// renders it.
func mutateEvent(t *testing.T, from, to string) (string, error) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(eventFixture, "action.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), from) {
		t.Fatalf("fixture action.go no longer contains %q", from)
	}
	dir := filepath.Join(moduleTempDir(t), "create_event")
	copyDir(t, eventFixture, dir)
	if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return Render(dir)
}

// TestRolesContract (A1, A3, T3): a role-restricted action says who may call
// it, quoting httpx.RolesRule, lists the 401 and 403 answers Bind gives
// before it reads the request, and lists the signed-in user and role with
// the values the server sets (never among the body fields); a Public action
// says anyone may call it, has no 401/403 line, and says what the user and
// role are when nobody is signed in.
func TestRolesContract(t *testing.T) {
	got, err := Render(eventFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The action \"Create event\" answers POST /events.\n\nWho may call it: signed-in users with role `organizer` or `admin`. Anyone else is answered with HTTP 403 (HTTP 401 if not signed in), and the action does not run: the server checks this before it reads the request.\n\n",
		"with these 2 fields and no others:\n- `title`: text.\n- `starts_at`: a whole number.\n",
		"- `user`: set by the server: the signed-in user (from the app's sign-in session); the caller does not send it, and a request that does is answered with HTTP 400 below.\n",
		"- `role`: set by the server: the signed-in user's role (from the app's sign-in session); the caller does not send it, and a request that does is answered with HTTP 400 below.\n",
		"- HTTP 401 Unauthorized, id \"unauthorized\", message \"sign-in required\", if the caller is not signed in. The action does not run.\n",
		"- HTTP 403 Forbidden, id \"forbidden\", message \"not allowed for this role\", if the caller is signed in with a role not listed above. The action does not run.\n",
		"`organizer_id` = the signed-in user, `created_as` = the signed-in user's role,",
		"the answer's `organizer_id` equals the signed-in user (\"the event belongs to the signed-in user\")",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	// One role: no "or".
	got, err = mutateEvent(t, `httpx.Roles("organizer", "admin")`, `httpx.Roles("admin")`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Who may call it: signed-in users with role `admin`. Anyone else"; !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
	// Public: anyone; no 401/403 line; the user and role say their signed-out value.
	// (In the fixture app, events is owned (A4) and a Public action never
	// writes an owned table, so this runs in an app without the annotation.)
	root := tempApp(t, withoutOwner(t), "")
	got, err = Render(addSlice(t, root, eventFixture, "create_event", "action.go", `httpx.Roles("organizer", "admin")`, `httpx.Public`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Who may call it: anyone, signed in or not.\n",
		"- `user`: set by the server: the signed-in user (from the app's sign-in session), or 0 when the caller is not signed in; the caller does not send it",
		"- `role`: set by the server: the signed-in user's role (from the app's sign-in session), or the empty text when the caller is not signed in;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "HTTP 401") || strings.Contains(got, "HTTP 403") {
		t.Errorf("a Public action has no 401/403 answer:\n%s", got)
	}
}

// TestRolesRefusals (A1, A2, T3): every other form is refused with its rule.
func TestRolesRefusals(t *testing.T) {
	const roles = `var Roles = httpx.Roles("organizer", "admin")`
	const user = "User     int64  `json:\"user\" server:\"user\"`"
	cases := map[string]struct{ from, to, want string }{
		"slice of strings": {roles, `var Roles = []string{"admin"}`,
			`refused: Roles declaration Roles = []string{"admin"} is not in the allowed pattern list (A1 who may call it)`},
		"typed var": {roles, `var Roles httpx.Access = httpx.Public`,
			`refused: Roles declaration Roles httpx.Access = httpx.Public is not in the allowed pattern list (A1 who may call it)`},
		"other httpx value": {roles, `var Roles = httpx.Forbidden`,
			`refused: Roles declaration Roles = httpx.Forbidden is not in the allowed pattern list (A1 who may call it)`},
		"spread": {roles, `var Roles = httpx.Roles(names...)`,
			`is not in the allowed pattern list (A1 who may call it)`},
		"not a literal": {roles, `var Roles = httpx.Roles(Route)`,
			`refused: role Route that is not a string literal is not in the allowed pattern list (A1 who may call it)`},
		"upper case": {roles, `var Roles = httpx.Roles("Admin")`,
			`refused: role "Admin" that is not a lowercase identifier is not in the allowed pattern list (A1 who may call it)`},
		"hyphen": {roles, `var Roles = httpx.Roles("box-office")`,
			`refused: role "box-office" that is not a lowercase identifier`},
		"twice": {roles, `var Roles = httpx.Roles("admin", "admin")`,
			`refused: role "admin" listed twice is not in the allowed pattern list (A1 who may call it)`},
		"second declaration": {roles, roles + "\n\nvar (\n\tRoles = httpx.Public\n)",
			`refused: second Roles declaration is not in the allowed pattern list (A1 who may call it)`},
		"unknown role": {roles, `var Roles = httpx.Roles("organiser")`,
			`refused: role "organiser" that cmd/server does not declare is not in the allowed pattern list (A2 app roles). The app's roles are declared once, in cmd/server/routes.go:`},
		"httpx in Handle": {"if in.Title == \"\" {", "if in.Role == httpx.PublicRule {",
			`refused: package-level value httpx.PublicRule is not in the allowed pattern list (expression)`},
		"role is int64": {"Role     string `json:\"role\" server:\"role\"`", "Role     int64  `json:\"role\" server:\"role\"`",
			`refused: server field Role of type int64 is not in the allowed pattern list (T3 user, role passed in)`},
		"user is bool": {user, "User     bool   `json:\"user\" server:\"user\"`",
			`refused: server field User of type bool is not in the allowed pattern list (T3 user, role passed in)`},
		"user named otherwise": {user, "User     int64  `json:\"owner\" server:\"user\"`",
			`refused: server:"user" field User with json name "owner" is not in the allowed pattern list (T3 user, role passed in)`},
		"user from the query": {user, "User     int64  `json:\"user\" query:\"user\" server:\"user\"`",
			`refused: server field User with a path or query tag is not in the allowed pattern list (T3 user, role passed in)`},
		"second user": {user, user + "\n\tOther    int64  `json:\"user\" server:\"user\"`",
			`refused: second user field Other is not in the allowed pattern list (T3 user, role passed in)`},
		"user in the body": {user, "User     int64  `json:\"user\"`",
			`refused: field User with json name "user" that the caller sends is not in the allowed pattern list (T3 user, role passed in). Who the caller is never comes from the request.`},
		"user on output": {"CreatedAs   string `json:\"created_as\"`", "CreatedAs   string `json:\"created_as\" server:\"role\"`",
			`refused: server tag on CreatedAs outside Input is not in the allowed pattern list (T3 user, role passed in)`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := mutateEvent(t, tc.from, tc.to)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
}

// TestAppRoles (A2): the app's roles are declared once in cmd/server with
// httpx.AppRoles; a role-restricted action in an app without it is refused,
// and so is a malformed declaration. A Public action needs none.
func TestAppRoles(t *testing.T) {
	routes := func(decl string) string {
		return "package main\n\nimport \"github.com/pierre10101/go-ai-bridge/runtime/httpx\"\n\n" + decl + "\n"
	}
	cases := map[string]struct{ routes, roles, want string }{
		"none, restricted": {"", `httpx.Roles("admin")`,
			`refused: role "admin" without an app-wide role list is not in the allowed pattern list (A2 app roles). Declare the app's roles once in cmd/server: var AppRoles = httpx.AppRoles("<role>", ...), and pass it to httpx.Identify`},
		"none, public": {"", `httpx.Public`, ""},
		"declared":     {routes(`var AppRoles = httpx.AppRoles("admin")`), `httpx.Roles("admin")`, ""},
		"upper case": {routes(`var AppRoles = httpx.AppRoles("Admin", "admin")`), `httpx.Roles("admin")`,
			`cmd/server/routes.go:5:31: refused: app role "Admin" that is not a lowercase identifier is not in the allowed pattern list (A2 app roles)`},
		"twice": {routes(`var AppRoles = httpx.AppRoles("admin", "admin")`), `httpx.Roles("admin")`,
			`refused: app role "admin" listed twice is not in the allowed pattern list (A2 app roles)`},
		"empty": {routes(`var AppRoles = httpx.AppRoles()`), `httpx.Public`,
			`refused: httpx.AppRoles without roles is not in the allowed pattern list (A2 app roles)`},
		"two calls": {routes("var AppRoles = httpx.AppRoles(\"admin\")\nvar More = httpx.AppRoles(\"customer\")"), `httpx.Roles("admin")`,
			`refused: second httpx.AppRoles call (the first is at cmd/server/routes.go:5:16) is not in the allowed pattern list (A2 app roles)`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			copyDir(t, "testdata/internal", filepath.Join(root, "internal"))
			gomod, _ := os.ReadFile("testdata/go.mod")
			os.WriteFile(filepath.Join(root, "go.mod"), []byte(strings.Replace(string(gomod), "=> ../..", "=> "+mustAbs(t, ".."), 1)), 0o644)
			os.WriteFile(filepath.Join(root, "schema.sql"), []byte(withoutOwner(t)), 0o644) // A2 only: no A4 owner
			if tc.routes != "" {
				os.MkdirAll(filepath.Join(root, "cmd", "server"), 0o755)
				os.WriteFile(filepath.Join(root, "cmd", "server", "routes.go"), []byte(tc.routes), 0o644)
			}
			dir := filepath.Join(root, "features", "create_event")
			copyDir(t, eventFixture, dir)
			src, _ := os.ReadFile(filepath.Join(dir, "action.go"))
			os.WriteFile(filepath.Join(dir, "action.go"), []byte(strings.Replace(string(src), `httpx.Roles("organizer", "admin")`, tc.roles, 1)), 0o644)
			_, err := Render(dir)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
