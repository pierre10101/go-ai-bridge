package httpx

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// Server-set inputs. An Input field tagged `clock:"now"` (grammar T1) or
// `server:"session"` (grammar T2) is filled by Bind, never by the caller:
// a request whose body or query string names such a field (in any letter
// case) is answered with BadInput and the action does not run.

// SessionCookie is the one cookie Bind reads a `server:"session"` Input field
// from. The name is fixed so the English can name it.
//
// Bind only reads it: it never mints, signs or renews the cookie and checks
// it against no store. The app issues it (for example when it serves its
// web page), with an unguessable value (a random id), HttpOnly, SameSite=Lax
// and, over HTTPS, Secure; whoever presents the value is that session.
const SessionCookie = "bridge_session"

// SessionRule is how Bind fills an Input field tagged `server:"session"`
// (quoted by bridge-en, grammar T2). bridge-en fills {zero} and {valid}
// from SessionValue for the field's type.
const SessionRule = "set by the server from the session cookie `" + SessionCookie + "`: {valid}, or {zero} when the request has no such cookie, has it more than once, or its value is anything else; the caller does not send it, and a request that does is answered with HTTP 400 below"

// SessionValue says, per Go type of a `server:"session"` field, which cookie
// values are a session (Valid) and what the field is otherwise (Zero).
var SessionValue = map[string]struct{ Valid, Zero string }{
	"int64":  {Valid: "its value when that is a whole number from 1 up, written in digits only", Zero: "0"},
	"string": {Valid: "its value when that is 1 to 128 letters, digits, '-', '_' or '.'", Zero: "the empty text"},
}

// ServerSetWhen is the extra BadInput condition of an action with a
// server-set input (quoted by bridge-en after BadInput.When or BadQueryWhen).
const ServerSetWhen = "the request sends a value that the server sets, in the body or in the query string"

// Server-set tags and their only values.
const (
	clockTag   = "clock"
	serverTag  = "server"
	sessionTag = "session"
)

// maxSessionText is the longest text session Bind accepts.
const maxSessionText = 128

// isServerSet reports whether f is filled by Bind (clock or session).
func isServerSet(f reflect.StructField) bool {
	return f.Tag.Get(clockTag) != "" || f.Tag.Get(serverTag) != ""
}

// serverSetSent returns a message when the caller sends a server-set field
// of I in the query string (any method), comparing names case-insensitively;
// "" otherwise. The body is checked by required.
func serverSetSent[I any](r *http.Request) string {
	t := reflect.TypeOf(*new(I))
	if t == nil || t.Kind() != reflect.Struct {
		return ""
	}
	query := r.URL.Query()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !isServerSet(f) {
			continue
		}
		name := fieldName(f)
		for k := range query {
			if strings.EqualFold(k, name) {
				return serverSetMessage(f, name)
			}
		}
	}
	return ""
}

// serverSetMessage is the BadInput message for a caller-sent server field.
func serverSetMessage(f reflect.StructField, name string) string {
	what := "the current time"
	if f.Tag.Get(serverTag) != "" {
		what = "the session cookie " + SessionCookie
	}
	return fmt.Sprintf("field %q is set by the server (%s); do not send it", name, what)
}

// fieldName is f's json name (or its Go name without one).
func fieldName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "" {
		name = f.Name
	}
	return name
}

// stampSession sets every `server:"session"` field of *in from the request's
// SessionCookie (SessionRule). Any other server tag or type is a bug in the
// slice (bridge-en refuses it too): an assertion, so HTTP 500.
func stampSession[I any](in *I, r *http.Request) {
	v := reflect.ValueOf(in).Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Tag.Get(serverTag) == "" {
			continue
		}
		k := f.Type.Kind()
		assert.Pre(f.Tag.Get(serverTag) == sessionTag && (k == reflect.Int64 || k == reflect.String),
			"a server field is int64 or string tagged server:\"session\"")
		raw, ok := sessionCookie(r)
		switch k {
		case reflect.Int64:
			n, valid := parseSessionInt(raw)
			if !ok || !valid {
				n = 0
			}
			v.Field(i).SetInt(n)
		case reflect.String:
			if !ok || !validSessionText(raw) {
				raw = ""
			}
			v.Field(i).SetString(raw)
		}
	}
}

// sessionCookie returns the value of the request's only SessionCookie; ok is
// false when it is missing or sent more than once.
func sessionCookie(r *http.Request) (string, bool) {
	value, n := "", 0
	for _, c := range r.Cookies() {
		if c.Name == SessionCookie {
			value, n = c.Value, n+1
		}
	}
	return value, n == 1
}

// parseSessionInt accepts digits only (no sign, no spaces), 1..MaxInt64.
func parseSessionInt(s string) (int64, bool) {
	if s == "" || len(s) > 19 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 1
}

// validSessionText accepts 1..maxSessionText of [A-Za-z0-9._-].
func validSessionText(s string) bool {
	if s == "" || len(s) > maxSessionText {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}
