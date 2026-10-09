// Package adapter is the deterministic Go -> English compiler ("bridge-en").
//
// It parses a slice's action.go with go/ast, accepts only the patterns in
// Grammar, and renders fixed English templates (templates.go). There is no AI
// and no heuristics: anything outside the grammar is refused with the
// construct and its file:line:col, and CI fails.
package adapter

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
)

// Pattern is one entry of the allowed pattern list. The list IS the grammar.
type Pattern struct {
	ID    string
	Name  string
	Shape string
}

// Grammar is the complete allowed pattern list for a slice: action.go (D, S,
// E), queries/*.sql (Q) and what the adapter reads outside the slice: the
// app's internal/domain (M) and cmd/server, bound with the runtime's httpx (H).
var Grammar = []Pattern{
	// Declarations (top level of action.go).
	{"D1", "package", "package <snake_case_name>"},
	{"D2", "imports", "only context, net/http, github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page}, <module>/internal/domain, <module>/features/<name>/db; no renamed, dot or blank imports"},
	{"D3", "route", `const Route = "<METHOD> /<path>"`},
	{"D4", "input", "type Input struct { <Field> <type> `json:\"<name>\"` ... }; at most 10 fields; type is int64, string, bool or domain.<T>; every field is required (H1: left out or null is HTTP 400), except an int64 field tagged clock:\"now\" (T1), which the server sets"},
	{"D5", "output", "type Output struct { ... } with the same field rules as D4, plus []domain.<T> list fields (one page of rows)"},
	{"D6", "failures", `var ( F<n> = failure.New("F<n>", http.Status<Name>, "<message>") ... )`},
	{"D7", "action", "type Action struct { q *db.Queries }"},
	{"D8", "constructor", "func New(q *db.Queries) *Action { return &Action{q: q} }"},
	{"D9", "handle", "func (a *Action) Handle(ctx context.Context, in Input) (Output, error) { S... }"},
	// Statements (body of Handle). Rendered as numbered Steps in code order.
	{"S1", "precondition", `assert.Pre(<cond>, "<reason>"); only at the top of Handle; zero or more; must not read in (see "meaningful precondition")`},
	{"S2", "guard", "if <cond> { return Output{}, F<n> }"},
	{"S3", "query", "<name>, err := a.q.<Query>(ctx, <value>...); <Query> is a Q1-Q3, Q5 or Q6 query"},
	{"S4", "error return", "if err != nil { return Output{}, err }; immediately after every S3"},
	{"S5", "let", "<name> := <value>"},
	{"S6", "postcondition", `assert.Post(<cond>, "<reason>"); after the last S2-S5, at least one`},
	{"S7", "success return", "return <name>, nil; the last statement"},
	{"S8", "map each", "<items> := make([]domain.<T>, len(<rows>)); for <i>, <row> := range <rows> { <items>[<i>] = domain.<T>{...} }; only over a Q5 :many result; English: for each row"},
	// Expressions.
	{"S9", "next cursor", `<name> := page.NextAfter(<Q5 result>, "<cursor column>", <the Q5 LIMIT value>); after the Q5 query; at most one; the keyset cursor of the next page (0 when there is none)`},
	{"S10", "claim check", "after a Q6 claim <n>, err := a.q.<Query>(...): an S2 guard if <n> != 1 { return Output{}, F<n> } before the success return; <n> is compared only as != 1, == 1 or == 0; English: not exactly one <row> was changed in step <k>"},
	{"E1", "name", "<local> or <local>.<Field>[.<Field>]"},
	{"E2", "literal", "integer, string, true, false, nil"},
	{"E3", "comparison", "== != < <= > >="},
	{"E4", "logic", "&& || !"},
	{"E5", "domain call", "domain.<Func>(<value>...); <Func> is an M2 domain function, rendered from its body; or page.IsPageLimit(<value>), a runtime primitive: <value> is between 1 and page.MaxPageSize (both included)"},
	{"E6", "record", "<Type>{<Field>: <value>, ...}; keyed; Type is Output, domain.<T> or db.<T>Params"},
	{"E7", "parentheses", "(<expr>)"},
	// SQL (queries/*.sql). Anything else is refused with file:line:col.
	{"Q0", "query", "-- name: <Query> :one|:many|:execrows, then exactly one statement; :one is Q1-Q3; :many is only Q5 (keyset page); :execrows is only Q6 (claim)"},
	{"Q1", "count", "SELECT COUNT(*) FROM <table> WHERE <col> = <value> [AND <col> = <value>]..."},
	{"Q2", "one row", "SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND <col> = <value>]..."},
	{"Q3", "insert", "INSERT INTO <table> (<col>, ...) VALUES (<value>, ...) RETURNING <col>, ...; a value may also be (SELECT COALESCE(MAX(<col>), 0) + 1 FROM <table>) for the same column and table"},
	{"Q4", "value", "? or sqlc.arg(<name>) (a parameter), an integer, or 'text'"},
	{"Q5", "keyset page", "SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND ...] AND <cursor> < <value> ORDER BY <cursor> DESC LIMIT <n>; :many only; no OFFSET; <n> is a parameter or 1..page.MaxPageSize; the action guards the limit with page.IsPageLimit; the English states page.MaxPageSize and page.DefaultPageSize; a :many without LIMIT is refused; GET slices only"},
	{"Q6", "claim update", "UPDATE <table> SET <col> = <value>, ... WHERE <cond> [AND <cond>]...; <cond> is <col> = <> < <= > >= <value>, or one parenthesised (<cond> OR <cond> ...) group; at least one <col> = <parameter> outside the group; a parameter may be +/- a whole number (sqlc.arg(now) - 600); next to the current time (T1) a comparison is said as a distance: <= now - d is 'd or more before', < 'more than d before', > 'later than d before', >= 'no earlier than d before'; >= now + d is 'd or more after', > 'more than d after', < 'earlier than d after', <= 'no later than d after'; :execrows only, no RETURNING; the check and the write are one statement; checked by S10"},
	// Rules across statements.
	{"T1", "time is passed in", "action.go and internal/domain never import time or read a clock; the current time is an Input field Now int64 `json:\"now\" clock:\"now\"` set by the server (httpx.ClockRule); next to it a Q6 offset in seconds is said in minutes, hours or days"},
	{"W1", "no check-then-write", "a Q3 or Q6 write to a table that an earlier Q1, Q2 or Q5 query of the same action read is refused; put the condition into the write (Q6) and check the changed-row count (S10)"},
	// Read from outside the slice.
	{"M1", "domain type", "every internal/domain type an action uses has a `// bridge-en: <display name>` doc line: a noun phrase, no parentheses; the only hand-written English in internal/domain"},
	{"M2", "domain function", `every func in internal/domain: assert.Pre/Post(<cond>, "<reason>")..., then return <expr>; or (bool) switch <param> { case "<text>", ...: return true } then return false; English is rendered from the body; no bridge-en comment, no methods, no package-level vars; imports only fmt and runtime/{assert,shape}`},
	{"M3", "domain expression", `<param>, literal, literal const, <T>(<expr>) conversion, == != < <= > >=, && || !, call to an M2 function, shape.Has(<text>, "<shape>") (# is one digit; a runtime primitive proven by runtime/shape tests), fmt.Sprintf("<format>", <expr>...) with %d, %0<n>d, %s`},
	{"H1", "http plumbing", "github.com/pierre10101/go-ai-bridge/runtime/httpx declares BadInput, Internal, SuccessStatus, InputRule, QueryInputRule, BadQueryWhen, TxRule, ReadTxRule, ClockRule and ErrorBody; GET binds path/query (QueryInputRule) and runs in a read-only transaction (ReadTxRule); cmd/server binds every Route with the runtime's httpx.Bind over queries built on its txn.DB (one transaction per call); the app's go.mod pins the runtime at the binary's version"},
}

// A meaningful precondition (S1) states something the caller guarantees and
// no request can break: a broken one is a bug in our code, never a user error.
// So it must not read the request (in): request values are checked by S2
// guards with an F-ID. Wiring checks like "a.q is not nil" are allowed but
// add little; a slice with nothing real to assert has no S1 at all.
// clockHint is attached to every T1 refusal: logic never reads the clock.
const clockHint = "Logic never reads the clock. Take the current time as an Input field (Now int64 `json:\"now\" clock:\"now\"`, set by the server; checks pass any time they like) and compare against it"

// checkThenWriteHint is attached to every W1 refusal.
const checkThenWriteHint = "Do not read a row and then write it: another call can change it in between. Put the condition into the write itself (one Q6 claim: UPDATE <table> SET ... WHERE <col> = ? AND <condition>) and stop unless exactly one row changed (S10)"

// claimCheckHint is attached to every S10 refusal.
const claimCheckHint = "After a Q6 claim, stop unless exactly one row changed: if <changed> != 1 { return Output{}, F<n> }"

const meaningfulPrecondition = "A precondition is what the caller guarantees and no request can break; request values are user input, so check them with an S2 guard and an F-ID"

// Hard limits checked by the adapter (no waivers).
const (
	MaxHandleLines = 70
	MaxFileLines   = 300
	MaxInputFields = 10
)

// Page sizes are not declared here: the only source is runtime/page
// (MaxPageSize, DefaultPageSize), which bridge-en is built with (pageSizes).

const allowedStmts = "S1 precondition, S2 guard, S3 query, S4 error return, S5 let, S6 postcondition, S7 success return, S8 map each, S9 next cursor"

// Refusal is one construct the adapter cannot render.
type Refusal struct {
	Pos       token.Position
	Construct string // what was found, e.g. "for loop"
	Context   string // where, e.g. "Handle body"
	Hint      string // what is allowed instead
}

func (r Refusal) Error() string {
	msg := fmt.Sprintf("%s: refused: %s is not in the allowed pattern list (%s)", r.Pos, r.Construct, r.Context)
	if r.Hint != "" {
		msg += ". " + r.Hint
	}
	return msg
}

// Refusals is the error returned when anything is unrenderable.
type Refusals []Refusal

func (rs Refusals) Error() string {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i].Pos, rs[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	lines := make([]string, len(rs))
	for i, r := range rs {
		lines[i] = r.Error()
	}
	return strings.Join(lines, "\n")
}

// GrammarText renders the allowed pattern list (bridge-en -grammar).
func GrammarText() string {
	var b strings.Builder
	for _, p := range Grammar {
		fmt.Fprintf(&b, "%-4s %-19s %s\n", p.ID, p.Name, p.Shape)
	}
	return b.String()
}
