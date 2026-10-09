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
	// The intent, written first (features/<slice>/intent.md). -check and -write read it before action.go.
	{"I1", "intent first", "every slice has intent.md; -check and -write refuse a slice without one before they read action.go"},
	{"I2", "failure cases", "intent.md has exactly one heading \"## Failure cases\" with one line per failure case, \"- F<n>: <text>\", F-IDs in increasing order, each once; a long case continues on lines indented by two spaces; no failure case: the single line \"None.\"; no F-ID list item anywhere else in intent.md; the rest of intent.md is free English"},
	{"I3", "intent = code", "the F-IDs listed in intent.md are exactly the F-IDs action.go declares (D6), each missing or extra ID refused by name; every F-ID is covered by a check in checks/ (func TestF<n>_... that references <slice>.F<n>)"},
	// Declarations (top level of action.go).
	{"D1", "package", "package <snake_case_name>"},
	{"D2", "imports", "only context, net/http, github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page,httpx} (httpx only for the A1 Roles declaration), <module>/internal/domain, <module>/features/<name>/db; no renamed, dot or blank imports"},
	{"D3", "route", `const Route = "<METHOD> /<path>"`},
	{"D4", "input", "type Input struct { <Field> <type> `json:\"<name>\"` ... }; at most 10 fields; type is int64, string, bool, domain.<T> or a D10 list ([]int64 or []string tagged list:\"<min>..<max>\"); every field is required (H1: left out or null is HTTP 400), except the server-set inputs: an int64 field tagged clock:\"now\" (T1), one int64 or string field tagged server:\"session\" (T2), one int64 or string field `json:\"user\" server:\"user\"` and one string field `json:\"role\" server:\"role\"` (T3), which the server sets and the caller must not send"},
	{"D5", "output", "type Output struct { ... } with the same field rules as D4, plus []domain.<T> list fields (one page of rows)"},
	{"D6", "failures", `var ( F<n> = failure.New("F<n>", http.Status<Name>, "<message>") ... ); with Roles (A1), the only package-level variables`},
	{"D7", "action", "type Action struct { q *db.Queries }"},
	{"D8", "constructor", "func New(q *db.Queries) *Action { return &Action{q: q} }"},
	{"D9", "handle", "func (a *Action) Handle(ctx context.Context, in Input) (Output, error) { S... }"},
	{"D10", "list input", "an Input field <Field> []int64 (or []string) `json:\"<name>\" list:\"<min>..<max>\"` with 1 <= min <= max <= httpx.MaxListLen (100); POST/PUT/PATCH/DELETE only; httpx.Bind answers HTTP 400 when the list has fewer than min or more than max entries, has the same entry twice, has an entry that is null or of the wrong type (httpx.ListWhen); English (httpx.ListRule): a list of <min> to <max> whole numbers (text values) with no duplicates; used only as the list of a Q7 IN and in the S11 check"},
	// Statements (body of Handle). Rendered as numbered Steps in code order.
	{"S1", "precondition", `assert.Pre(<cond>, "<reason>"); only at the top of Handle; zero or more; must not read in (see "meaningful precondition")`},
	{"S2", "guard", "if <cond> { return Output{}, F<n> }"},
	{"S3", "query", "<name>, err := a.q.<Query>(ctx, <value>...); <Query> is a Q1-Q3, Q5 or Q6 query; the list of a Q7 IN takes exactly a D10 list input in.<List>"},
	{"S4", "error return", "if err != nil { return Output{}, err }; immediately after every S3"},
	{"S5", "let", "<name> := <value>"},
	{"S6", "postcondition", `assert.Post(<cond>, "<reason>"); after the last S2-S5, at least one`},
	{"S7", "success return", "return <name>, nil; the last statement"},
	{"S8", "map each", "<items> := make([]domain.<T>, len(<rows>)); for <i>, <row> := range <rows> { <items>[<i>] = domain.<T>{...} }; only over a Q5 :many result; English: for each row"},
	// Expressions.
	{"S9", "next cursor", `<name> := page.NextAfter(<Q5 result>, "<cursor column>", <the Q5 LIMIT value>); after the Q5 query; at most one; the keyset cursor of the next page (0 when there is none)`},
	{"S10", "claim check", "after a Q6 claim <n>, err := a.q.<Query>(...): an S2 guard whose entire condition is <n> != 1, if <n> != 1 { return Output{}, F<n> }, before the success return; the same test inside a compound condition (&&, ||, !) does not count, though such guards may stay as extra guards; <n> is compared only as != 1, == 1 or == 0; English: not exactly one <row> was changed in step <k>; a step that may stop says what happens to the claim's write: a guard with the conjunct <n> == 0 'Nothing was written in step <k>', a step before the count is checked (a read, the != 1 guard) 'Any change made in step <k> is rolled back', a step after it 'The write in step <k> is rolled back'"},
	{"S11", "multi-row claim check", "after a Q6 claim over <key> IN (sqlc.slice(<name>)) (Q7) whose list is the D10 input in.<List>: an S2 guard whose entire condition is <n> != int64(len(in.<List>)), if <n> != int64(len(in.<List>)) { return Output{}, F<n> }, before the success return; inside a compound condition it does not count; <n> is compared only in this check or as == 0; len appears nowhere else; English: the number of <rows> changed in step <k> is not the number of <rows> in the request's `<list>`; the rollback lines are those of S10"},
	{"E1", "name", "<local> or <local>.<Field>[.<Field>]"},
	{"E2", "literal", "integer, string, true, false, nil"},
	{"E3", "comparison", "== != < <= > >=; against the current time on the right (also inside a domain function called with it) said as no later than, earlier than, later than, no earlier than, exactly, not exactly the current time"},
	{"E4", "logic", "&& || !"},
	{"E5", "domain call", "domain.<Func>(<value>...); <Func> is an M2 domain function, rendered from its body; or page.IsPageLimit(<value>), a runtime primitive: <value> is between 1 and page.MaxPageSize (both included); no other call (len only as int64(len(in.<List>)) in the S11 check)"},
	{"E6", "record", "<Type>{<Field>: <value>, ...}; keyed; Type is Output, domain.<T> or db.<T>Params"},
	{"E7", "parentheses", "(<expr>)"},
	// SQL (queries/*.sql). Anything else is refused with file:line:col.
	{"Q0", "query", "-- name: <Query> :one|:many|:execrows, then exactly one statement; :one is Q1-Q3; :many is only Q5 (keyset page); :execrows is only Q6 (claim)"},
	{"Q1", "count", "SELECT COUNT(*) FROM <table> WHERE <col> = <value> [AND <col> = <value>]...; a condition may also compare a column with the server-set current time, <col> <op> sqlc.arg(now) [+ or - <seconds>] with <op> <> < <= > >= (or = with an offset), the column first, when the action passes exactly its clock input in.Now (T1) for that parameter (any other value, a literal, the parameter on the left, an OR, or the comparison after a Q7 IN list is refused); English: the Q6 boundary words (`expires_at` is no later than the current time), and a guard on the count says there is (no | at least one) <row> whose ..."},
	{"Q2", "one row", "SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND <col> = <value>]...; a condition may also compare a column with the server-set current time as in Q1 (<col> <op> sqlc.arg(now) [+ or - <seconds>], bound to in.Now), said in the Q6 boundary words"},
	{"Q3", "insert", "INSERT INTO <table> (<col>, ...) VALUES (<value>, ...) RETURNING <col>, ...; a value may also be (SELECT COALESCE(MAX(<col>), 0) + 1 FROM <table>) for the same column and table"},
	{"Q4", "value", "? or sqlc.arg(<name>) (a parameter), an integer, or 'text'"},
	{"Q5", "keyset page", "SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND ...] AND <cursor> < <value> ORDER BY <cursor> DESC LIMIT <n>; :many only; no OFFSET; no comparison other than the cursor (not with the current time either); <n> is a parameter or 1..page.MaxPageSize; the action guards the limit with page.IsPageLimit; the English states page.MaxPageSize and page.DefaultPageSize; a :many without LIMIT is refused; GET slices only"},
	{"Q6", "claim update", "UPDATE <table> SET <col> = <value>, ... WHERE <cond> [AND <cond>]...; <cond> is <col> = <> < <= > >= <value>, or one parenthesised (<cond> OR <cond> ...) group; at least one <col> = <parameter> outside the group; a parameter may be +/- a whole number (SET expires_at = sqlc.arg(now) + 600); next to the current time (T1) a comparison is said as a distance: <= now - d is 'd or more before', < 'more than d before', > 'later than d before', >= 'no earlier than d before'; >= now + d is 'd or more after', > 'more than d after', < 'earlier than d after', <= 'no later than d after'; with no offset, <= now is 'no later than the current time', < 'earlier than', > 'later than', >= 'no earlier than'; the same words in a Q1 count or Q2 one-row read; :execrows only, no RETURNING; the check and the write are one statement; checked by S10 (or, over a Q7 IN list, S11)"},
	{"Q7", "IN list", "<col> IN (sqlc.slice(<name>)) as one AND condition of a Q1, Q2 or Q6 WHERE (never inside an OR group); at most one per query; it is the last parameter of the statement (sqlc numbers parameters as if the slice were one value, so with SQLite a parameter after it would bind to a list entry); the action passes exactly a D10 list input for it; in a Q6 claim <col> is the table's single-column PRIMARY KEY in schema.sql, so each entry is at most one row; English: `<col>` is one of the request's `<list>`"},
	// Rules across statements.
	{"T1", "time is passed in", "action.go and internal/domain never import time or read a clock; the current time is an Input field Now int64 `json:\"now\" clock:\"now\"` set by the server (httpx.ClockRule), never sent by the caller (body or query string: HTTP 400); next to it a Q6 (or Q1, Q2) offset in seconds is said in minutes, hours or days; a read compares a column with the current time only through a parameter bound to in.Now itself"},
	{"T2", "session is passed in", "the caller's session is at most one Input field Session string (or int64) `json:\"session\" server:\"session\"`, set by httpx.Bind from the cookie httpx.SessionCookie (bridge_session) as httpx.SessionRule says: the empty text (or 0) without exactly one valid cookie, so the action's own S2 guard (if in.Session == \"\", or <= 0) raises its failure; it and the signed-in user (T3) are the only identity of the caller: a claim or check never takes who the caller is from a request field (no person_id or user_id input); a request whose body or query string sends it, in any letter case, is HTTP 400; the English lists it with the values the server sets, never among the fields the caller sends, and calls it the session from the cookie"},
	{"T3", "user, role passed in", "the signed-in user is at most one Input field User int64 (or string) `json:\"user\" server:\"user\"` and their role at most one field Role string `json:\"role\" server:\"role\"`, set by httpx.Bind from the app's sign-in hook (httpx.Identify) as httpx.UserRule and httpx.RoleRule say; for a Public action (A1) 0 / the empty text when nobody is signed in (httpx.SignedOutRule); a request whose body or query string sends user or role, in any letter case, is HTTP 400; an Input field the caller sends may not be named user, role, user_id or role_id; the English calls them the signed-in user and the signed-in user's role; sign-in, passwords and sessions stay in the app"},
	{"W1", "no check-then-write", "a Q3 or Q6 write to a table that an earlier Q1, Q2 or Q5 query of the same action read is refused; put the condition into the write (Q6) and check the changed-row count (S10)"},
	// Who may call the action (action.go and cmd/server).
	{"A1", "who may call it", "every action.go declares, once, var Roles = httpx.Roles(\"<role>\", ...) (one or more distinct lowercase identifiers [a-z][a-z0-9_]* as string literals: signed-in users with one of these roles) or var Roles = httpx.Public (anyone, signed in or not); no declaration, an empty list or any other form is refused (deny by default); English: httpx.RolesRule (who may call it: signed-in users with role <roles>; anyone else is answered with HTTP 403, HTTP 401 if not signed in, and the action does not run) or httpx.PublicRule"},
	{"A2", "app roles", "the app declares its roles once, in cmd/server: var AppRoles = httpx.AppRoles(\"<role>\", ...) (distinct lowercase identifiers as string literals), passed to httpx.Identify; a role an action lists that AppRoles does not declare is refused (a typo is caught), and so is a role list in an app without AppRoles; at runtime a role the sign-in hook returns that AppRoles does not declare counts as not signed in"},
	{"A3", "enforced before Handle", "cmd/server binds every Route with httpx.Bind(<slice>.Roles, <slice>.New(...).Handle), its own Roles, and serves the mux through httpx.Identify(AppRoles, <the app's sign-in hook>, mux); Bind checks the caller before it reads the request: not signed in and not Public, HTTP 401 httpx.Unauthenticated; a role not listed, HTTP 403 httpx.Forbidden; Handle does not run, so nothing is written; without httpx.Identify nobody is signed in"},
	// Read from outside the slice.
	{"M1", "domain type", "every internal/domain type an action uses has a `// bridge-en: <display name>` doc line: a noun phrase, no parentheses; the only hand-written English in internal/domain"},
	{"M2", "domain function", `every func in internal/domain: assert.Pre/Post(<cond>, "<reason>")..., then return <expr>; or (bool) switch <param> { case "<text>", ...: return true } then return false; English is rendered from the body; no bridge-en comment, no methods, no package-level vars; imports only fmt and runtime/{assert,shape}`},
	{"M3", "domain expression", `<param>, literal, literal const, <T>(<expr>) conversion, == != < <= > >=, && || !, call to an M2 function, shape.Has(<text>, "<shape>") (# is one digit; a runtime primitive proven by runtime/shape tests), fmt.Sprintf("<format>", <expr>...) with %d, %0<n>d, %s`},
	{"H1", "http plumbing", "github.com/pierre10101/go-ai-bridge/runtime/httpx declares BadInput, Internal, SuccessStatus, InputRule, QueryInputRule, BadQueryWhen, TxRule, ReadTxRule, ClockRule, SessionCookie, SessionRule, SessionValue, ServerSetWhen, ListRule, ListRuleExact, ListElems, ListWhen, PublicRule, RolesRule, Unauthenticated, Forbidden, UserRule, RoleRule, SignedOutRule, SignedOutZero and ErrorBody, and Bind, Identify, AppRoles, Roles and Public; GET binds path/query (QueryInputRule) and runs in a read-only transaction (ReadTxRule); cmd/server binds every Route with the runtime's httpx.Bind(<slice>.Roles, ...) over queries built on its txn.DB (one transaction per call; A3); the app's go.mod pins the runtime at the binary's version"},
}

// A meaningful precondition (S1) states something the caller guarantees and
// no request can break: a broken one is a bug in our code, never a user error.
// So it must not read the request (in): request values are checked by S2
// guards with an F-ID. Wiring checks like "a.q is not nil" are allowed but
// add little; a slice with nothing real to assert has no S1 at all.
// clockHint is attached to every T1 refusal: logic never reads the clock.
const clockHint = "Logic never reads the clock. Take the current time as an Input field (Now int64 `json:\"now\" clock:\"now\"`, set by the server; checks pass any time they like) and compare against it"

// sessionHint is attached to every T2 refusal.
const sessionHint = "Write: Session string `json:\"session\" server:\"session\"` (or int64), set by the server from the session cookie (httpx.SessionCookie); checks pass any session they like"

// checkThenWriteHint is attached to every W1 refusal.
const checkThenWriteHint = "Do not read a row and then write it: another call can change it in between. Put the condition into the write itself (one Q6 claim: UPDATE <table> SET ... WHERE <col> = ? AND <condition>) and stop unless exactly one row changed (S10)"

// claimCheckHint is attached to every S10 refusal.
const claimCheckHint = "After a Q6 claim, stop unless exactly one row changed: if <changed> != 1 { return Output{}, F<n> }"

// rolesHint is attached to every A1 refusal.
const rolesHint = "Every action declares who may call it, once, at the top level of action.go: var Roles = httpx.Roles(\"<role>\", ...) (signed-in users with one of these roles; lowercase identifiers declared app-wide with httpx.AppRoles in cmd/server) or var Roles = httpx.Public (anyone, signed in or not); import github.com/pierre10101/go-ai-bridge/runtime/httpx for it"

// appRolesHint is attached to A2 refusals.
const appRolesHint = "Declare the app's roles once in cmd/server: var AppRoles = httpx.AppRoles(\"<role>\", ...), and pass it to httpx.Identify"

// userHint is attached to T3 refusals.
const userHint = "The server sets who the caller is: User int64 `json:\"user\" server:\"user\"` (or string) is the signed-in user and Role string `json:\"role\" server:\"role\"` their role; httpx.Bind fills both from the app's sign-in hook (httpx.Identify), and a request that sends them is HTTP 400"

// identityHint is attached to a body field named like the signed-in user or role.
const identityHint = "Who the caller is never comes from the request. " + userHint + ". A field about someone else takes another name (for example member_id)"

// listHint is attached to every D10 refusal.
const listHint = "An Input list is []int64 or []string tagged with its bounds, list:\"<min>..<max>\" with 1 <= min <= max <= 100, for example SeatIDs []int64 `json:\"seat_ids\" list:\"1..20\"`"

// listUseHint is attached to a list input used anywhere else than Q7 and S11.
const listUseHint = "A list input (D10) is only passed to an IN (sqlc.slice(<name>)) parameter (Q7) and counted in the S11 check: if <changed> != int64(len(in.<List>)) { return Output{}, F<n> }"

// multiCheckHint is attached to S11 refusals.
const multiCheckHint = "After a claim over <key> IN (sqlc.slice(<name>)) (Q7), stop unless it changed one row per entry of the list: if <changed> != int64(len(in.<List>)) { return Output{}, F<n> }"

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
	sortRefusals(rs)
	lines := make([]string, len(rs))
	for i, r := range rs {
		lines[i] = r.Error()
	}
	return strings.Join(lines, "\n")
}

// sortRefusals orders refusals by file, line and column.
func sortRefusals(rs Refusals) {
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
}

// GrammarText renders the allowed pattern list (bridge-en -grammar).
func GrammarText() string {
	var b strings.Builder
	for _, p := range Grammar {
		fmt.Fprintf(&b, "%-4s %-22s %s\n", p.ID, p.Name, p.Shape)
	}
	return b.String()
}
