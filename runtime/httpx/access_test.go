package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// These tests prove PublicRule, RolesRule, Unauthenticated, Forbidden,
// UserRule, RoleRule and SignedOutRule, the sentences bridge-en quotes for
// an action's declared Roles (grammar A1-A3) and its `server:"user"` and
// `server:"role"` Input fields (grammar T3).

type userIn struct {
	Title string `json:"title"`
	User  int64  `json:"user" server:"user"`
	Role  string `json:"role" server:"role"`
}

type userOut struct {
	User int64  `json:"user"`
	Role string `json:"role"`
}

var testRoles = AppRoles("customer", "organizer", "admin")

// testIdentity is a stand-in for the app's sign-in hook: it reads the
// headers X-User and X-Role (a real app reads its own session store).
func testIdentity(r *http.Request) (string, string, bool) {
	u, ok := r.Header["X-User"]
	if !ok {
		return "", "", false
	}
	return u[0], r.Header.Get("X-Role"), true
}

type call struct {
	method, target, body string
	user, role           string // "" user: no X-User header (not signed in)
}

func serveAccess[I any](t *testing.T, access Access, identity Identity, c call) (*httptest.ResponseRecorder, *I) {
	t.Helper()
	var got *I
	mux := http.NewServeMux()
	route := c.method + " /x"
	mux.Handle(route, Bind(access, func(_ context.Context, in I) (userOut, error) {
		got = &in
		return userOut{}, nil
	}))
	h := Identify(testRoles, identity, mux)
	if c.target == "" {
		c.target = "/x"
	}
	req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
	if c.user != "" {
		req.Header.Set("X-User", c.user)
		req.Header.Set("X-Role", c.role)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, got
}

func wantOutcome(t *testing.T, rec *httptest.ResponseRecorder, o Outcome) {
	t.Helper()
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	if rec.Code != o.Status || body.Error.ID != o.ID || (o.Message != "" && body.Error.Message != o.Message) {
		t.Fatalf("status %d %s; want %d %q", rec.Code, rec.Body, o.Status, o.ID)
	}
}

// RolesRule: "signed-in users with role {roles}. Anyone else is answered
// with HTTP 403 (HTTP 401 if not signed in), and the action does not run:
// the server checks this before it reads the request."
func TestRolesRule(t *testing.T) {
	access := Roles("organizer", "admin")
	body := `{"title":"x"}`
	// Not signed in: no hook, a hook that says no, a role the app does not
	// declare, a user id that is not valid text, an int64 user that is not
	// a whole number from 1 up: 401, and Handle does not run.
	for name, tc := range map[string]struct {
		identity Identity
		user     string
		role     string
	}{
		"no Identify hook":        {nil, "7", "admin"},
		"hook says not signed in": {testIdentity, "", ""},
		"undeclared role":         {testIdentity, "7", "superuser"},
		"empty role":              {testIdentity, "7", ""},
		"bad user text":           {testIdentity, "a b", "admin"},
		"int64 user not a number": {testIdentity, "abc", "admin"},
		"int64 user zero":         {testIdentity, "0", "admin"},
	} {
		rec, got := serveAccess[userIn](t, access, tc.identity, call{method: "POST", body: body, user: tc.user, role: tc.role})
		if got != nil {
			t.Fatalf("%s: the action ran", name)
		}
		wantOutcome(t, rec, Unauthenticated)
	}
	// Signed in with a role not listed: 403, before the request is read
	// (a bad body, or one that sends `user`, is not even looked at).
	for _, b := range []string{body, `not json`, `{"title":"x","user":1}`} {
		rec, got := serveAccess[userIn](t, access, testIdentity, call{method: "POST", body: b, user: "7", role: "customer"})
		if got != nil {
			t.Fatalf("%s: the action ran", b)
		}
		wantOutcome(t, rec, Forbidden)
	}
	// A listed role: the action runs with the signed-in user and role.
	for _, role := range []string{"organizer", "admin"} {
		rec, got := serveAccess[userIn](t, access, testIdentity, call{method: "POST", body: body, user: "7", role: role})
		if rec.Code != http.StatusCreated || got == nil || got.User != 7 || got.Role != role || got.Title != "x" {
			t.Fatalf("%s: status %d %s, in %+v", role, rec.Code, rec.Body, got)
		}
	}
	// GET routes are checked the same way.
	type getIn struct {
		User string `json:"user" server:"user"`
	}
	rec, got := serveAccess[getIn](t, access, testIdentity, call{method: "GET", user: "u-1", role: "customer"})
	if got != nil {
		t.Fatal("GET: the action ran")
	}
	wantOutcome(t, rec, Forbidden)
	rec, got = serveAccess[getIn](t, access, testIdentity, call{method: "GET", user: "u-1", role: "admin"})
	if rec.Code != http.StatusOK || got == nil || got.User != "u-1" {
		t.Fatalf("GET admin: %d %s %+v", rec.Code, rec.Body, got)
	}
	if !strings.Contains(RolesRule, "{roles}") || !strings.Contains(RolesRule, "HTTP 403 (HTTP 401 if not signed in)") {
		t.Fatalf("RolesRule %q", RolesRule)
	}
}

// Deny by default: the zero Access (an action that declared nothing) lets
// nobody in, signed in or not.
func TestZeroAccessDeniesEveryone(t *testing.T) {
	rec, got := serveAccess[userIn](t, Access{}, testIdentity, call{method: "POST", body: `{"title":"x"}`})
	if got != nil {
		t.Fatal("ran")
	}
	wantOutcome(t, rec, Unauthenticated)
	rec, got = serveAccess[userIn](t, Access{}, testIdentity, call{method: "POST", body: `{"title":"x"}`, user: "1", role: "admin"})
	if got != nil {
		t.Fatal("ran")
	}
	wantOutcome(t, rec, Forbidden)
}

// PublicRule and SignedOutRule: anyone may call a Public action; the user
// and role are 0 / the empty text when nobody is signed in, and the
// signed-in user's when someone is.
func TestPublicRule(t *testing.T) {
	rec, got := serveAccess[userIn](t, Public, nil, call{method: "POST", body: `{"title":"x"}`})
	if rec.Code != http.StatusCreated || got == nil || got.User != 0 || got.Role != "" {
		t.Fatalf("signed out: %d %s %+v", rec.Code, rec.Body, got)
	}
	rec, got = serveAccess[userIn](t, Public, testIdentity, call{method: "POST", body: `{"title":"x"}`, user: "9", role: "customer"})
	if rec.Code != http.StatusCreated || got == nil || got.User != 9 || got.Role != "customer" {
		t.Fatalf("signed in: %d %s %+v", rec.Code, rec.Body, got)
	}
	type textIn struct {
		User string `json:"user" server:"user"`
	}
	rec, gotText := serveAccess[textIn](t, Public, testIdentity, call{method: "POST", body: `{}`})
	if rec.Code != http.StatusCreated || gotText == nil || gotText.User != "" {
		t.Fatalf("text signed out: %d %s %+v", rec.Code, rec.Body, gotText)
	}
	if SignedOutZero["int64"] != "0" || SignedOutZero["string"] != "the empty text" || !strings.Contains(SignedOutRule, "{zero}") {
		t.Fatalf("SignedOutRule %q %v", SignedOutRule, SignedOutZero)
	}
	for _, rule := range []string{UserRule, RoleRule} {
		if !strings.Contains(rule, "{signed out}") || !strings.HasSuffix(rule, "a request that does is answered with HTTP 400 below") {
			t.Fatalf("rule %q", rule)
		}
	}
}

// UserRule, RoleRule, ServerSetWhen: a request that sends `user` or `role`,
// in the body or the query string, in any letter case, is HTTP 400 and the
// action does not run, signed in with an allowed role or not.
func TestUserAndRoleAreNeverSent(t *testing.T) {
	for _, c := range []call{
		{method: "POST", body: `{"title":"x","user":7}`},
		{method: "POST", body: `{"title":"x","User":7}`},
		{method: "POST", body: `{"title":"x","role":"admin"}`},
		{method: "POST", body: `{"title":"x","ROLE":"admin"}`},
		{method: "POST", target: "/x?user=7", body: `{"title":"x"}`},
		{method: "POST", target: "/x?Role=admin", body: `{"title":"x"}`},
	} {
		for _, access := range []Access{Public, Roles("admin")} {
			c.user, c.role = "7", "admin"
			rec, got := serveAccess[userIn](t, access, testIdentity, c)
			if got != nil {
				t.Fatalf("%+v: the action ran", c)
			}
			wantOutcome(t, rec, BadInput)
			if !strings.Contains(rec.Body.String(), "is set by the server (the signed-in user") {
				t.Fatalf("%+v: message %s", c, rec.Body)
			}
		}
	}
	type getIn struct {
		User int64  `json:"user" server:"user"`
		Role string `json:"role" server:"role"`
	}
	for _, target := range []string{"/x?user=7", "/x?role=admin", "/x?USER=1"} {
		rec, got := serveAccess[getIn](t, Public, testIdentity, call{method: "GET", target: target})
		if got != nil {
			t.Fatalf("%s: the action ran", target)
		}
		wantOutcome(t, rec, BadInput)
	}
}

// A role list is lowercase identifiers, at least one, each once; anything
// else panics when the app starts (bridge-en refuses it before that).
func TestRoleDeclarationsPanic(t *testing.T) {
	for name, f := range map[string]func(){
		"Roles()":          func() { Roles() },
		"Roles(Admin)":     func() { Roles("Admin") },
		"Roles(twice)":     func() { Roles("admin", "admin") },
		"Roles(space)":     func() { Roles("an admin") },
		"AppRoles()":       func() { AppRoles() },
		"AppRoles(1st)":    func() { AppRoles("1st") },
		"AppRoles(twice)":  func() { AppRoles("a", "a") },
		"AppRoles(hyphen)": func() { AppRoles("box-office") },
		"Bypass()":         func() { AppRoles("admin").BypassOwnership() },
		"Bypass(unknown)":  func() { AppRoles("admin").BypassOwnership("root") },
		"Bypass(twice)":    func() { AppRoles("admin").BypassOwnership("admin", "admin") },
	} {
		func() {
			defer func() {
				if _, ok := recover().(assert.Violation); !ok {
					t.Errorf("%s did not panic with an assertion", name)
				}
			}()
			f()
		}()
	}
	if a := Roles("organizer", "admin"); a.IsPublic() || strings.Join(a.List(), ",") != "organizer,admin" {
		t.Fatalf("Roles: %+v", a)
	}
	set := AppRoles("organizer", "admin").BypassOwnership("admin")
	if !set.Has("organizer") || !set.Has("admin") || !set.BypassesOwnership("admin") || set.BypassesOwnership("organizer") {
		t.Fatalf("BypassOwnership: %+v", set)
	}
	if AppRoles("admin").BypassesOwnership("admin") {
		t.Fatal("no role bypasses ownership unless marked")
	}
	if !Public.IsPublic() || Public.List() != nil {
		t.Fatal("Public")
	}
	for _, ok := range []string{"admin", "box_office", "a1"} {
		if !ValidRoleName(ok) {
			t.Errorf("%q refused", ok)
		}
	}
}
