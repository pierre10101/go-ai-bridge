package httpx

import (
	"context"
	"log"
	"net/http"
	"reflect"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// Sign-in and roles (grammar T3, A1-A3).
//
// Password hashing, sign-in and sessions stay in the app: bridge-en never
// sees a password or a sign-in session. The app tells the runtime who is
// signed in through one hook, Identity, installed once around all routes
// with Identify. Bind then
//
//   - checks the caller against the action's declared Access (Public, or
//     Roles(...)) BEFORE it reads the request: HTTP 401 (Unauthenticated)
//     when the action is not Public and nobody is signed in, HTTP 403
//     (Forbidden) when the signed-in role is not listed. Handle does not run.
//   - fills the Input fields tagged `server:"user"` and `server:"role"`
//     (UserRule, RoleRule); a request that sends them is BadInput.
//
// Deny by default: a route without Identify has nobody signed in, the zero
// Access lists no role, and a role the app did not declare in AppRoles
// counts as not signed in.

// Identity is the app's sign-in hook. It returns the signed-in user's id
// (as text: digits for an int64 user field), their role and ok = true, or
// ok = false when nobody is signed in. It must not write to w or read the
// request body; it typically reads the app's own session cookie and looks
// the session up in the app's store.
type Identity func(r *http.Request) (user, role string, ok bool)

// RoleSet is every role a signed-in user of the app can have, declared
// once in cmd/server with AppRoles (grammar A2).
type RoleSet struct {
	names map[string]bool
}

// AppRoles declares the app's roles: lowercase identifiers
// ([a-z][a-z0-9_]*), at least one, each once. Anything else is a bug in
// the app (bridge-en refuses it too): it panics when the app starts.
func AppRoles(names ...string) RoleSet {
	assert.Pre(len(names) > 0, "httpx.AppRoles declares at least one role")
	set := RoleSet{names: map[string]bool{}}
	for _, n := range names {
		assert.Pre(ValidRoleName(n), "a role name is a lowercase identifier: "+n)
		assert.Pre(!set.names[n], "httpx.AppRoles lists each role once: "+n)
		set.names[n] = true
	}
	return set
}

// Has reports whether the app declared role.
func (s RoleSet) Has(role string) bool { return s.names[role] }

// Access is who may call one action: Public, or Roles(...). An action
// declares it as `var Roles = httpx.Roles("organizer", "admin")` or
// `var Roles = httpx.Public` (grammar A1) and cmd/server passes it to Bind
// (A3). The zero Access lists no role and is not public: it denies everyone.
type Access struct {
	public bool
	roles  []string
}

// Public is the Access of an action anyone may call, signed in or not.
var Public = Access{public: true}

// Roles is the Access of an action only signed-in users with one of these
// roles may call: lowercase identifiers, at least one, each once (else it
// panics when the app starts).
func Roles(names ...string) Access {
	assert.Pre(len(names) > 0, "httpx.Roles lists at least one role (use httpx.Public for an action anyone may call)")
	seen := map[string]bool{}
	for _, n := range names {
		assert.Pre(ValidRoleName(n), "a role name is a lowercase identifier: "+n)
		assert.Pre(!seen[n], "httpx.Roles lists each role once: "+n)
		seen[n] = true
	}
	return Access{roles: append([]string(nil), names...)}
}

// IsPublic reports whether anyone may call the action.
func (a Access) IsPublic() bool { return a.public }

// List is the roles an action allows (nil for Public).
func (a Access) List() []string { return append([]string(nil), a.roles...) }

func (a Access) allows(role string) bool {
	for _, r := range a.roles {
		if r == role {
			return true
		}
	}
	return false
}

// ValidRoleName: a lowercase identifier, [a-z][a-z0-9_]*, at most 32 bytes.
func ValidRoleName(s string) bool {
	if s == "" || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// PublicRule is the English of an action declared httpx.Public (quoted by
// bridge-en, grammar A1).
const PublicRule = "Who may call it: anyone, signed in or not."

// RolesRule is the English of an action declared httpx.Roles(...) (quoted
// by bridge-en, grammar A1, A3). bridge-en fills {roles}.
const RolesRule = "Who may call it: signed-in users with role {roles}. Anyone else is answered with HTTP 403 (HTTP 401 if not signed in), and the action does not run: the server checks this before it reads the request."

// Unauthenticated: the action is not Public and nobody is signed in. The
// action does not run.
var Unauthenticated = Outcome{
	Status:  http.StatusUnauthorized,
	ID:      "unauthorized",
	Message: "sign-in required",
	When:    "the caller is not signed in",
	Note:    "The action does not run.",
}

// Forbidden: the caller is signed in, but their role is not one the action
// lists. The action does not run.
var Forbidden = Outcome{
	Status:  http.StatusForbidden,
	ID:      "forbidden",
	Message: "not allowed for this role",
	When:    "the caller is signed in with a role not listed above",
	Note:    "The action does not run.",
}

// UserRule and RoleRule are how Bind fills an Input field tagged
// `server:"user"` or `server:"role"` (quoted by bridge-en, grammar T3).
// {signed out} is "" for an action declared with Roles (it never runs
// signed out) and SignedOutRule for a Public one.
const (
	UserRule = "set by the server: the signed-in user (from the app's sign-in session){signed out}; the caller does not send it, and a request that does is answered with HTTP 400 below"
	RoleRule = "set by the server: the signed-in user's role (from the app's sign-in session){signed out}; the caller does not send it, and a request that does is answered with HTTP 400 below"
)

// SignedOutRule says what a user or role field is when nobody is signed in
// (Public actions only); {zero} comes from SignedOutZero by the field's type.
const SignedOutRule = ", or {zero} when the caller is not signed in"

// SignedOutZero is the value of a user or role field when nobody is signed in.
var SignedOutZero = map[string]string{"int64": "0", "string": "the empty text"}

// Server tag values of T3.
const (
	userTag = "user"
	roleTag = "role"
)

// caller is who Identify found signed in.
type caller struct{ user, role string }

type callerKey struct{}

// Identify installs the app's sign-in hook in front of next (once, around
// the mux of every route). For each request it calls identity and, when it
// returns ok with a user id of 1 to 128 letters, digits, '-', '_' or '.' and
// a role the app declared in roles, Bind sees that user as signed in. In
// every other case (a nil identity included) nobody is signed in. A request
// cannot set this itself: only Identify writes it.
func Identify(roles RoleSet, identity Identity, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), callerKey{}, (*caller)(nil))
		if identity != nil {
			user, role, ok := identity(r)
			switch {
			case !ok:
			case !validSessionText(user):
				log.Printf("httpx.Identify: the sign-in hook returned an invalid user id; treated as not signed in")
			case !roles.Has(role):
				log.Printf("httpx.Identify: the sign-in hook returned role %q, which httpx.AppRoles does not declare; treated as not signed in", role)
			default:
				ctx = context.WithValue(r.Context(), callerKey{}, &caller{user: user, role: role})
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// signedIn returns the caller Identify found for r, or nil. An int64 user
// field needs a user id that is a whole number from 1 up; otherwise the
// caller counts as not signed in for this action.
func signedIn[I any](r *http.Request) *caller {
	c, _ := r.Context().Value(callerKey{}).(*caller)
	if c == nil {
		return nil
	}
	t := reflect.TypeOf(*new(I))
	if t != nil && t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Tag.Get(serverTag) == userTag && f.Type.Kind() == reflect.Int64 {
				if _, ok := parseSessionInt(c.user); !ok {
					return nil
				}
			}
		}
	}
	return c
}

// denied returns the outcome Bind answers before reading the request, or
// false when the caller may call the action.
func (a Access) denied(c *caller) (Outcome, bool) {
	switch {
	case a.public:
		return Outcome{}, false
	case c == nil:
		return Unauthenticated, true
	case !a.allows(c.role):
		return Forbidden, true
	}
	return Outcome{}, false
}
