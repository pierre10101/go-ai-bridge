// Package httpx binds an action's Handle method to net/http.
//
// Apps import it (github.com/pierre10101/go-ai-bridge/runtime/httpx); the
// bridge-en version in the app's go.mod pins it. This is plumbing outside
// features/, so generics and recover are fine here. Feature code never
// touches http.Request or http.ResponseWriter.
//
// bridge-en is built from the same module version and quotes the
// declarations below (BadInput, Internal, SuccessStatus, InputRule,
// QueryInputRule, BadQueryWhen, StrictQueryRule, TxRule, ReadTxRule, ErrorBody, ClockRule in
// clock.go, SessionCookie, SessionRule, SessionValue and ServerSetWhen in
// session.go, PublicRule, RolesRule, Unauthenticated, Forbidden, UserRule,
// RoleRule, SignedOutRule and SignedOutZero in access.go, and ListRule, ListRuleExact, ListElems and ListWhen in
// list.go) to write the HTTP and transaction sentences of every slice's
// .en file. Bind answers only through them, and the tests in this package
// prove each sentence, so the English cannot drift from what Bind does.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

// MaxBody is the largest request body Bind reads (1 MiB).
const MaxBody = 1 << 20

// Outcome is one answer Bind gives that is not an action's own F-ID failure.
type Outcome struct {
	Status  int
	ID      string // error.id in the body
	Message string // error.message; empty means "describes the problem" (varies)
	When    string // English: when Bind answers this way (quoted by bridge-en)
	Note    string // English: what else the caller should know (quoted by bridge-en)
}

// BadInput: the body could not become the action's Input. The action does not run.
var BadInput = Outcome{
	Status: http.StatusBadRequest,
	ID:     "bad_request",
	When:   "the body is not exactly one JSON object, leaves out a field listed above or sets one to null, has a field not listed above (names are case-sensitive), has a value of the wrong type, or is larger than 1 MiB",
	Note:   "The action does not run.",
}

// Internal: the action returned an error that is not a *failure.Failure (in
// the bridge grammar only a failed query does that), an assertion failed, or
// the transaction could not be committed.
// bridge-en names the steps that can cause it; the Note is quoted.
var Internal = Outcome{
	Status:  http.StatusInternalServerError,
	ID:      "internal",
	Message: "internal error",
	Note:    "The cause is written to the server log and is never sent to the caller.",
}

// SuccessStatus is the status of a successful answer, by route method.
var SuccessStatus = map[string]int{
	http.MethodGet:    http.StatusOK,
	http.MethodPost:   http.StatusCreated,
	http.MethodPut:    http.StatusOK,
	http.MethodPatch:  http.StatusOK,
	http.MethodDelete: http.StatusOK,
}

// InputRule is how Bind treats Input fields the caller did not send (quoted by bridge-en).
// Every field is required, also inside objects; decode enforces it.
const InputRule = "Every field is required, also inside objects: a field that is left out, or is null, is answered with HTTP 400 below and the action does not run."

// TxRule is how Bind runs Handle: through txn.Run, so every query of one call
// shares one transaction, begun by the first query with BEGIN IMMEDIATE (the
// write lock), committed after Handle succeeds and rolled back when it stops. bridge-en fills {first} (the first query step), {last} (the
// step before the success answer) and {commit} (the success answer step).
// ReadTxRule is how Bind runs a GET Handle: through txn.Read, a read-only
// transaction with no write lock ({first}, {last} and {end} are step numbers).
const ReadTxRule = "Steps {first} to {last} run in one read-only database transaction. It begins with the query in step {first} and takes no write lock; every query in it reads the same snapshot of the database, and a write inside it fails. It ends in step {end}, before the answer is sent."

const TxRule = "Steps {first} to {last} run in one database transaction. It begins with the query in step {first} and holds the database's write lock until it ends, so no other connection can write in between (other writers wait). If any of steps {first} to {last} stops the action, the transaction is rolled back and nothing is written. It is committed in step {commit}, before the answer is sent."

// ErrorBody is the JSON shape of every error response.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries the failure ID (F1, F2, ...) and its message.
type ErrorDetail struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// Bind turns Handle(ctx, Input) (Output, error) into an http.Handler:
// first who may call it (access, the action's declared Roles: RolesRule,
// Unauthenticated, Forbidden; before the request is read), then strict
// JSON in (every field required), Handle run in one transaction (txn.Run:
// committed on success, rolled back on any error or failed assertion), JSON
// out, *failure.Failure mapped to its status.
func Bind[I any, O any](access Access, handle func(context.Context, I) (O, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverViolation(w)
		c := signedIn[I](r) // Identify: the app's sign-in hook
		if o, denied := access.denied(c); denied {
			writeOutcome(w, o, "") // RolesRule: Handle does not run
			return
		}
		var in I
		msg := serverSetSent[I](r) // ClockRule, SessionRule, UserRule, RoleRule: never from the query string
		if msg == "" && r.Method == http.MethodGet {
			msg = unknownQuery[I](r) // StrictQueryRule (T4): no query parameter the action does not declare
		}
		if msg == "" {
			if r.Method == http.MethodGet && hasParamTags[I]() {
				in, msg = decodeParams[I](r)
			} else {
				in, msg = decode[I](w, r)
			}
		}
		if msg != "" {
			writeOutcome(w, BadInput, msg)
			return
		}
		stampClock(&in)        // ClockRule
		stampServer(&in, r, c) // SessionRule, UserRule, RoleRule
		run := txn.Run[O]
		if r.Method == http.MethodGet {
			run = txn.Read[O] // ReadTxRule
		}
		out, err := run(r.Context(), func(ctx context.Context) (O, error) { return handle(ctx, in) })
		var f *failure.Failure
		switch {
		case errors.As(err, &f):
			writeJSON(w, f.Status, ErrorBody{ErrorDetail{f.ID, f.Message}})
		case err != nil:
			log.Printf("internal error: %v", err)
			writeOutcome(w, Internal, "")
		default:
			writeJSON(w, SuccessStatus[r.Method], out)
		}
	})
}

// decode reads exactly one JSON object into I: only known fields, spelled
// exactly as their json names, and every field present and not null (also
// inside nested objects). It returns a description of the problem, or "".
func decode[I any](w http.ResponseWriter, r *http.Request) (I, string) {
	var in I
	var raw json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	if err := dec.Decode(&raw); err != nil {
		return in, err.Error()
	}
	if err := dec.Decode(&json.RawMessage{}); err != io.EOF {
		return in, "request body must be exactly one JSON object"
	}
	if raw = bytes.TrimSpace(raw); len(raw) == 0 || raw[0] != '{' {
		return in, "request body must be a JSON object"
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&in); err != nil {
		return in, err.Error()
	}
	var missing []string
	if msg := required(reflect.TypeOf(in), raw, "", &missing); msg != "" {
		return in, msg
	}
	switch len(missing) {
	case 0:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return in, err.Error()
		}
		return in, checkLists(reflect.ValueOf(in), obj) // ListRule, ListWhen
	case 1:
		return in, fmt.Sprintf("required field %s is missing or null", missing[0])
	}
	return in, fmt.Sprintf("required fields %s are missing or null", strings.Join(missing, ", "))
}

// required walks one JSON object against struct type t. encoding/json matches
// keys case-insensitively and leaves absent or null fields at their zero
// value; required refuses both. It returns a message for a key that is not
// exactly a field's json name, and appends every field that is left out or
// null to missing (quoted, dotted for nested objects).
func required(t reflect.Type, raw json.RawMessage, prefix string, missing *[]string) string {
	if t.Kind() != reflect.Struct {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err.Error()
	}
	names := map[string]bool{}
	type field struct {
		name string
		typ  reflect.Type
	}
	var fields []field
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		names[name] = true
		if isServerSet(f) {
			for k := range obj {
				if strings.EqualFold(k, name) { // encoding/json matches names in any case
					return serverSetMessage(f, prefix+name)
				}
			}
			continue // ClockRule, SessionRule: filled by Bind, never required from the caller
		}
		fields = append(fields, field{name, f.Type})
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !names[k] {
			return fmt.Sprintf("json: unknown field %q (field names are case-sensitive)", prefix+k)
		}
	}
	for _, f := range fields {
		v, ok := obj[f.name]
		if !ok || string(bytes.TrimSpace(v)) == "null" {
			*missing = append(*missing, fmt.Sprintf("%q", prefix+f.name))
			continue
		}
		if msg := required(f.typ, v, prefix+f.name+".", missing); msg != "" {
			return msg
		}
	}
	return ""
}

func writeOutcome(w http.ResponseWriter, o Outcome, msg string) {
	if o.Message != "" {
		msg = o.Message
	}
	writeJSON(w, o.Status, ErrorBody{ErrorDetail{o.ID, msg}})
}

func recoverViolation(w http.ResponseWriter) {
	v := recover()
	if v == nil {
		return
	}
	viol, ok := v.(assert.Violation)
	if !ok {
		panic(v)
	}
	log.Printf("BUG: %v", viol)
	writeOutcome(w, Internal, "")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
