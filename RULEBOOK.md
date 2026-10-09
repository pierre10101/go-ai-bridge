# bridge-en rulebook

This file is two things at once:

1. **The prompt** for anyone (person or AI) writing a feature slice: write
   only what is listed here.
2. **The parser contract**: `bridge-en` accepts exactly these patterns, renders
   each with a fixed English template and refuses everything else with
   `file:line:col`. There is no AI and no guessing in `bridge-en`.

`bridge-en -grammar` prints the same list in one line per rule. Every rule ID
below is also a heading, and `TestRulebookCoversGrammar` fails if the two
drift apart. This rulebook ships with each release (attached to the GitHub
Release and inside each archive) and with the module source, so the rules an
app follows are the rules of the version its `go.mod` pins.

Each rule has a short code example, the English `bridge-en` writes for it,
and one example it refuses. The English is quoted from real output: the
golden files `adapter/testdata/good/<slice>/<slice>.en` of the fixture app
(see [The fixture app](#the-fixture-app)).

## Contents

- [Install and pin](#install-and-pin)
- [App layout](#app-layout)
- [Writing a slice](#writing-a-slice)
- [Intent: I1-I3](#intent-i1-i3)
- [Declarations: D1-D10](#declarations-d1-d10)
- [Statements: S1-S11](#statements-s1-s11)
- [Expressions: E1-E7](#expressions-e1-e7)
- [SQL: Q0-Q7](#sql-q0-q7)
- [Rules across statements: T1-T4, W1](#rules-across-statements-t1-t4-w1)
- [Who may call it: A1-A4](#who-may-call-it-a1-a4)
- [Outside the slice: M1-M3, H1](#outside-the-slice-m1-m3-h1)
- [Conditional claim and state-transition rules](#conditional-claim-and-state-transition-rules)
- [Hard limits](#hard-limits)
- [The fixture app](#the-fixture-app)
- [Agent instructions: bridge-en init](#agent-instructions-bridge-en-init)
- [Review: the pull request comment](#review-the-pull-request-comment)

## Install and pin

bridge-en is one Go module, `github.com/pierre10101/go-ai-bridge`, with two
parts an app uses:

- the **binary** `bridge-en` (`cmd/bridge-en`), which renders and checks slices;
- the **runtime** `github.com/pierre10101/go-ai-bridge/runtime/...`
  (`assert`, `failure`, `httpx`, `page`, `shape`, `store`, `txn`), which the
  app imports like any Go package. An app never copies bridge-en source.

```sh
# 1. In the app: depend on one version. go.mod is the pin.
go get github.com/pierre10101/go-ai-bridge@v0.4.0

# 2. Install the binary of the same version.
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.4.0
bridge-en -version                      # bridge-en 0.4.0
```

Then `bridge-en init` writes `AGENTS.md` and pointer files for AI agents
into the app (documents only; see
[Agent instructions](#agent-instructions-bridge-en-init)).

The app's `go.mod` then says:

```
require github.com/pierre10101/go-ai-bridge v0.4.0
```

In the app's CI, the `setup-bridge-en` action installs the binary of the
version `go.mod` requires (`go list -m github.com/pierre10101/go-ai-bridge`), or
the `version` you pass it:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: "1.24.x"
- uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.4.0   # version from go.mod
# or download the released binary and check its SHA256 instead of building it:
# - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.4.0
#   with: { method: release }
- run: bridge-en -check features/*/
```

On pull requests, the `pr-english` action comments each changed feature's
intent next to its English (see
[Review: the pull request comment](#review-the-pull-request-comment)).

Without GitHub Actions: `go install …@v<version>` as above, or download
`bridge-en_<version>_<os>_<arch>.tar.gz` and `SHA256SUMS` from the release and
check the hash.

**Why app pull requests cannot rewrite the English.** The English quotes the
runtime (for example `httpx.InputRule`, `httpx.TxRule`, `httpx.ClockRule`, `httpx.SessionRule`, `httpx.ListRule`, `httpx.RolesRule`, `httpx.UserRule`, `httpx.StrictQueryRule`),
and the app runs that same runtime: both come from the one module version in
`go.mod`, verified by the Go checksum database. The app has no copy to edit.
`bridge-en -check` (and `-write`) first refuse an app whose `go.mod` requires
another version than the binary, or none:

```
go.mod: pins github.com/pierre10101/go-ai-bridge v0.0.9 but this is bridge-en v0.4.0; install the pinned version (go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.0.9) or move the app (go get github.com/pierre10101/go-ai-bridge@v0.4.0), then review every .en diff
```

To move an app to a new version: `go get github.com/pierre10101/go-ai-bridge@v<new>`,
install the same binary, run `bridge-en -write` on every slice, review the
diff of every `.en` file, commit. 0.4.0 is a breaking change: a table can
declare its owner (`-- owner: <col>` in `schema.sql`) and then every write
to it from an action a non-admin role may call is limited to the signed-in
user's rows (A4), and a GET that sends a query parameter it does not declare
is answered with HTTP 400 (T4); the migration steps are in README.md,
"0.3.x to 0.4.0". 0.3.0 was one too: every action
declares who may call it (`var Roles = httpx.Roles(...)` or `httpx.Public`,
A1), the app declares its roles once in `cmd/server` (`httpx.AppRoles`, A2),
and `cmd/server` binds each route with `httpx.Bind(<slice>.Roles, ...)`
behind `httpx.Identify` (A3); the migration steps are in README.md, "0.2.x
to 0.3.0". (0.2.0 was too: every slice needs an `intent.md` in the I1-I3
format; see README.md, "0.1.x to 0.2.0".)

## App layout

`bridge-en` reads an app from its module root (the directory with `go.mod`):

```
go.mod                       any module path; requires github.com/pierre10101/go-ai-bridge (the pin)
schema.sql / schema.go       the app's schema (with "-- owner: <col>" above an owned table, A4); schema.go embeds it for store.Open
sqlc.yaml                    one entry per slice
cmd/server/main.go           store.Open(ctx, path, schema), then serve Routes(db, <the app's sign-in hook>)
cmd/server/routes.go         var AppRoles = httpx.AppRoles(...) (A2), .BypassOwnership(...) (A4); one line per slice:
                             mux.Handle(<slice>.Route, httpx.Bind(<slice>.Roles, ...txn.DB(db)...)) (A3);
                             return httpx.Identify(AppRoles, identity, mux)
features/<slice>/            one directory per action (below)
internal/domain/             the app's value objects and pure rules (M1-M3); may be absent
```

`httpx` and `txn` in `routes.go` are `github.com/pierre10101/go-ai-bridge/runtime/httpx`
and `.../runtime/txn`; `store` is `.../runtime/store`.

A slice:

```
features/<slice>/
  intent.md          why, inputs, outputs, "## Failure cases" F1..Fn (I1-I3; write this FIRST)
  action.go          Route, Roles, Input, Output, F-IDs, Action, New, Handle (D1-D10, S1-S11, A1)
  queries/*.sql      plain SQL with sqlc annotations (Q0-Q7)
  db/                sqlc-generated code (never edited by hand)
  checks/*_test.go   one TestF<n>_... per F-ID, referencing <slice>.F<n>
  <slice>.en         golden English (bridge-en -write), reviewed in the pull request
```

`bridge-en -check features/*/` reads `intent.md` first (I1, I2) and refuses
a slice whose F-IDs there differ from those of `action.go` (I3) before it
checks anything else. Then it also cross-checks: every F-ID is covered by a
check in `checks/`; every query in `queries/` is called (no dead SQL); the
route is bound with the runtime's `httpx.Bind`, with the slice's own `Roles`,
over its `txn.DB` (H1, A3).

Sign-in, password hashing and sessions are the app's own code, outside
`features/` and outside bridge-en. The app gives the runtime one hook,
`httpx.Identity` (who is signed in, with which role), installed once with
`httpx.Identify` around all routes; see [Who may call it](#who-may-call-it-a1-a4). Which rows of a table each signed-in user may change is declared in `schema.sql` (`-- owner: <col>`, A4).

## Writing a slice

1. Write `intent.md` with every failure case (F1..Fn) in English **before any
   code**, in the I2 format: `- F<n>: <text>` under `## Failure cases`.
2. Write `queries/*.sql` (Q0-Q7) and run `sqlc generate`.
3. Write `action.go` inside D1-D10 / S1-S11, declaring who may call it
   (`var Roles = httpx.Roles("<role>", ...)` or `httpx.Public`, A1).
4. Write one check per F-ID in `checks/` (`func TestF<n>_...` that references
   `<slice>.F<n>`).
5. Bind the route in `cmd/server/routes.go` with its own Roles:
   `httpx.Bind(<slice>.Roles, ...)` (A3).
6. `bridge-en -check features/<slice>` after every edit; a refusal names the
   rule to follow.
7. `bridge-en -write features/<slice>` (it refuses while I1-I3 fail); read
   the `.en` against `intent.md` like a reviewer would; commit it with the
   code. Never edit a `.en` by hand.

---

## Intent: I1-I3

`intent.md` is the request in plain English, written before the code. Most of
it is free English for the reviewer (why, who, inputs, outputs, out of
scope). bridge-en reads exactly one part of it, the failure cases, and holds
the code to them. `-check` and `-write` read `intent.md` **before**
`action.go` and stop at the first I1-I3 refusal; `-write` saves no English
until they pass.

### I1

**intent first** - every slice has `intent.md`.

Refused (`adapter/testdata/bad_intent/no_intent`):
```
testdata/bad_intent/no_intent/intent.md: refused: feature without intent.md is not in the allowed pattern list (I1 intent first). Write intent.md before any code: why, inputs, outputs and every failure case. Under the heading "## Failure cases", write one line per failure case, "- F<n>: <text>" (for example "- F1: the customer does not exist."), F-IDs in increasing order, each once; a long case continues on lines indented by two spaces; a slice with no failure case writes the single line "None."
```

### I2

**failure cases** - exactly one heading `## Failure cases` (this text, level
2). Up to the next heading, every non-blank line is:

- an entry `- F<n>: <text>`: a dash, a space, the F-ID (`F1`, `F2`, ...,
  no `F0`), a colon, a space, then the text;
- a continuation of the entry above, indented by two spaces;
- or, for a slice with no failure case, the single line `None.`

F-IDs appear in increasing order, each once. Nowhere else in `intent.md` may
a list item start with an F-ID (`- F1 ...`, `- **F1** ...`), so there is one
place to read them; prose that mentions F1 is fine.

```markdown
## Failure cases
- F1: the seat is held (by any session, this one included) and its hold
  expires later than now, or the seat does not exist: nothing changed.
- F2: there is no valid session cookie (the session is 0): nothing is
  written.
```

The first refusal in a file carries the whole format; the others say
`Expected "- F<n>: <text>" (the format is in the first refusal above)`.

Refused:
- the v0.1.4 style `- **F1** — the customer does not exist.` (`adapter/testdata/bad_intent/old_format`) -
  `intent.md:23:1: refused: line "- **F1** — the customer does not exist." in "## Failure cases" is not in the allowed pattern list (I2 failure cases). Under the heading "## Failure cases", write one line per failure case, "- F<n>: <text>" ...`
- no `## Failure cases` heading (`adapter/testdata/bad_intent/no_section`) -
  `intent.md: refused: intent.md without the heading "## Failure cases" is not in the allowed pattern list (I2 failure cases). ...`, and for each F-ID list item elsewhere,
  `intent.md:23:1: refused: failure case F1 outside "## Failure cases" ...`
- `- F1 the customer does not exist.` (no colon), `F2` after `F3`, `F2`
  twice, a prose line in the section (`adapter/testdata/bad_intent/malformed_entries`) -
  `refused: line "- F1 the customer does not exist." in "## Failure cases" ...`,
  `refused: failure case F2 after F3 ...`,
  `refused: second failure case F2 (the first is on line 25) ...`,
  `refused: line "Also anything else that goes wrong." in "## Failure cases" ...`
- `## Failure Cases` (another spelling) - `refused: heading "## Failure Cases" ... The heading is exactly "## Failure cases". ...`;
  a section with no entry and no `None.` - `refused: empty "## Failure cases" section ...`;
  a second heading - `refused: second "## Failure cases" heading (the first is on line 21) ...`

### I3

**intent = code** - the F-IDs listed under `## Failure cases` are exactly
the F-IDs `action.go` declares (D6). Each one missing on either side is
refused by name, at its line, with both sets. And, as before, every F-ID is
covered by a check in `checks/`: a `func TestF<n>_...` that references
`<slice>.F<n>` (and runs the action against SQLite).

Refused:
- action.go declares F3, intent.md lists F1, F2 (`adapter/testdata/bad_intent/missing_id`) -
  `testdata/bad_intent/missing_id/action.go:36:2: refused: failure case F3 that intent.md does not list is not in the allowed pattern list (I3 intent = code). intent.md lists F1, F2; action.go declares F1, F2, F3. Write the case in intent.md first ("- F3: <text>" under "## Failure cases"), or remove F3 from action.go`
- intent.md lists F4, action.go declares F1-F3 (`adapter/testdata/bad_intent/extra_id`) -
  `testdata/bad_intent/extra_id/intent.md:26:1: refused: failure case F4 that action.go does not declare is not in the allowed pattern list (I3 intent = code). intent.md lists F1, F2, F3, F4; action.go declares F1, F2, F3. Declare it in action.go (F4 = failure.New("F4", http.Status<Name>, "<message>")) and raise it with a guard, or remove it from intent.md`
- an F-ID no check covers - `checks: no check covers F2 (I3 intent = code): add func TestF2_... in checks/ that references create_invoice.F2 and runs against real SQLite`

---

## Declarations: D1-D10

Top level of `action.go`. Nothing else may be declared there.

### D1

**package** - `package <snake_case_name>`; the title of the English is the
package name in words.

```go
package create_invoice
```
English: `# Create invoice` ... `The action "Create invoice" answers POST /invoices.`

Refused: `package createInvoice` - `refused: package name "createInvoice" is not in the allowed pattern list (D1 package). Use snake_case`

### D2

**imports** - only `context`, `net/http`,
`github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page,httpx}`,
`<module>/internal/domain` and `<module>/features/<slice>/db`; no renamed,
dot or blank imports. `httpx` is only for the `Roles` declaration (A1):
`Handle` never uses it.

```go
import (
	"context"
	"net/http"

	"example.com/app/features/create_invoice/db"
	"example.com/app/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)
```
English: none (imports decide what can be said).

Refused: `import "fmt"` - `refused: import "fmt" is not in the allowed pattern list (D2 imports). Allowed: context, net/http, github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page,httpx}, <module>/internal/domain, <module>/features/hidden_magic/db`; `httpx.PublicRule` used in `Handle` - `refused: package-level value httpx.PublicRule is not in the allowed pattern list (expression)`

### D3

**route** - `const Route = "<METHOD> /<path>"`, the only constant.

```go
const Route = "POST /invoices"
```
English: `The action "Create invoice" answers POST /invoices.`

Refused: `const MaxRetries = 3` - `refused: constant declaration MaxRetries is not in the allowed pattern list (D3 route). The only constant is Route = "<METHOD> /<path>"`

### D4

**input** - `type Input struct` with at most 10 fields, each `int64`, `string`,
`bool`, `domain.<T>` or a list input (D10: `[]int64` or `[]string` tagged
`list:"<min>..<max>"`), each with a json tag. Every field is required (left
out or null is HTTP 400). Exception, the **server-set inputs**: an `int64`
field tagged `clock:"now"` (T1), one `int64` or `string` field tagged
`server:"session"` (T2), and the signed-in user `json:"user" server:"user"`
(`int64` or `string`) and role `json:"role" server:"role"` (`string`) (T3)
are set by the server; the caller must not send them
(body or query string: HTTP 400), and the English lists them apart from the
fields the caller sends. GET fields take `path:"<name>"` or `query:"<name>"`,
and a GET request that sends any other query parameter is answered with
HTTP 400 (T4).

```go
type Input struct {
	CustomerID  int64  `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}
```
English:
```
The request body is one JSON object with these 3 fields and no others:
- `customer_id`: a whole number.
- `amount_cents`: a whole number.
- `currency`: text.
Every field is required, also inside objects: a field that is left out, or is null, is answered with HTTP 400 below and the action does not run.
```

Refused: `Lines []int64 \`json:"lines"\`` (a list without its bounds) - `refused: list field Lines without a list tag is not in the allowed pattern list (D10 list input). An Input list is []int64 or []string tagged with its bounds, list:"<min>..<max>" with 1 <= min <= max <= 100, for example SeatIDs []int64 \`json:"seat_ids" list:"1..20"\``

### D5

**output** - `type Output struct`, same field rules as D4, plus
`[]domain.<T>` list fields (one page of rows).

```go
type Output struct {
	Invoices  []domain.InvoiceSummary `json:"invoices"`
	NextAfter int64                   `json:"next_after"`
}
```
English: ``- `invoices`: a list (possibly empty) of invoice summaries, each an object with `invoice_number` (an invoice number) and `total` (an amount of money).``

Refused: `Raw map[string]any \`json:"raw"\`` - `refused: field type map[string]any is not in the allowed pattern list (D5 output). Use int64, string, bool, domain.<T> or (on Output) []domain.<T>`

### D6

**failures** - `var ( F<n> = failure.New("F<n>", http.Status<Name>, "<message>") )`;
the only package-level variables besides `Roles` (A1); every F-ID is raised
by an S2 guard.
Statuses: 400, 401, 403, 404, 409, 410, 422, 429.

```go
var (
	F1 = failure.New("F1", http.StatusUnprocessableEntity, "customer does not exist")
)
```
English: `- F1 "customer does not exist": HTTP 422 Unprocessable Entity; step 4, before any write.`

Refused: `var retries = 3` - `refused: package-level variable retries is not in the allowed pattern list (D6 failures)`; an F-ID no guard raises - `refused: failure case F4 that no guard raises`

### D7

**action** - exactly `type Action struct { q *db.Queries }`.

Refused: `type Action struct { q *db.Queries; log *log.Logger }` - `refused: Action struct of a different shape is not in the allowed pattern list (D7 action)`

### D8

**constructor** - exactly `func New(q *db.Queries) *Action { return &Action{q: q} }`.

Refused: any other body or signature - `refused: constructor of a different shape is not in the allowed pattern list (D8 constructor)`

### D9

**handle** - exactly `func (a *Action) Handle(ctx context.Context, in Input) (Output, error)`,
its body only S1-S10, at most 70 lines. Other functions are refused: pure
helpers go to `internal/domain`.

Refused: `func vat(c int64) int64 { ... }` - `refused: function vat is not in the allowed pattern list (top level of action.go). Only New (D8) and Handle (D9); put pure helpers in internal/domain`

### D10

**list input** - an Input field `[]int64` or `[]string` with a json tag and
its bounds, `list:"<min>..<max>"`, `1 <= min <= max <= httpx.MaxListLen`
(100). The caller sends it in the JSON body (POST, PUT, PATCH, DELETE; never
GET). `httpx.Bind` (`runtime/httpx`) answers HTTP 400 `bad_request`, and the
action does not run, when the list has fewer than `<min>` or more than
`<max>` entries, has the same entry twice, has an entry that is null, or has
an entry of the wrong type (`httpx.ListWhen`; `runtime/httpx` `TestListWhenRefuses`).
In `Handle` a list input is used in exactly two places: as the list of a Q7
`IN (sqlc.slice(<name>))` and in the S11 check `int64(len(in.<List>))`.

```go
type Input struct {
	TicketIDs []int64 `json:"ticket_ids" list:"1..20"`
	Session   string  `json:"session" server:"session"`
	Now       int64   `json:"now" clock:"now"`
}
```
English (`adapter/testdata/good/confirm_many/confirm_many.en`, quoting
`httpx.ListRule` and `httpx.ListElems`; with `<min>` = `<max>`,
`httpx.ListRuleExact`: "a list of exactly 3 whole numbers with no duplicates"):
```
- `ticket_ids`: a list of 1 to 20 whole numbers with no duplicates.
```
and the 400 answer adds `; or a list has fewer or more entries than allowed
above, has the same entry twice, or has an entry that is null`.

Refused (`adapter/testdata/bad/list_shapes`):
- `NoTag []int64 \`json:"no_tag"\`` - `refused: list field NoTag without a list tag is not in the allowed pattern list (D10 list input). ...`
- `BadTag []int64 \`json:"bad_tag" list:"0..500"\`` - `refused: list field BadTag whose list tag "0..500" must have 1 <= min <= max <= 100 is not in the allowed pattern list (D10 list input). ...`
- `Flags []bool \`json:"flags" list:"1..3"\`` - `refused: list field Flags of type []bool is not in the allowed pattern list (D10 list input). ...`
- `Scalar int64 \`json:"scalar" list:"1..3"\`` - `refused: list tag on Scalar of type int64 ...`; on Output - `refused: list tag on Count outside Input ...`
- `if in.SeatIDs == nil { ... }` - `refused: list input in.SeatIDs used as a value is not in the allowed pattern list (D10 list input). A list input (D10) is only passed to an IN (sqlc.slice(<name>)) parameter (Q7) and counted in the S11 check: ...`
- a list in a GET action - `refused: list field TicketIDs in a GET action is not in the allowed pattern list (D10 list input). ...`

---

## Statements: S1-S11

The body of `Handle`. Rendered as numbered **Steps in code order**.

### S1

**precondition** - `assert.Pre(<cond>, "<reason>")`, only at the top. A
precondition is what the caller guarantees and no request can break, so it
must not read `in` (request values are checked by S2 guards with an F-ID).
A slice with nothing real to assert has none.

English: `## Preconditions` / `Checked first, before step 1. A false one is a bug, not a user error: the request stops with HTTP 500 Internal Server Error.` or `None asserted.`

Refused: `assert.Pre(in.AmountCents > 0, "positive")` - `refused: precondition that reads the request (in) is not in the allowed pattern list (S1 precondition)`

### S2

**guard** - `if <cond> { return Output{}, F<n> }`; no else, no init.

```go
if in.AmountCents <= 0 {
	return Output{}, F2
}
```
English: `1. If the request's `amount_cents` is at most 0, stop with F2: HTTP 422 Unprocessable Entity "amount must be greater than zero".`

Refused: `if ... { } else { }` - `refused: if/else statement is not in the allowed pattern list (Handle body)`

### S3

**query** - `<name>, err := a.q.<Query>(ctx, <value>...)` where `<Query>` is a
Q1-Q3, Q5 or Q6 query; arguments by position or as `db.<Query>Params{...}`.

```go
customers, err := a.q.CustomerExists(ctx, in.CustomerID)
```
English: `3. Read: count the customers whose `id` is the request's `customer_id` (query `CustomerExists` in queries/customer_exists.sql). If the query fails, stop with HTTP 500 Internal Server Error.`

Refused: a query with no `queries/*.sql` definition - `refused: query Foo with no queries/*.sql definition is not in the allowed pattern list (S3 query)`

### S4

**error return** - `if err != nil { return Output{}, err }`, immediately after every S3.

English: part of the S3 sentence ("If the query fails, stop with HTTP 500 ...").

Refused: an S3 without it - `refused: query whose error is not checked is not in the allowed pattern list (S3 query)`

### S5

**let** - `<name> := <value>`; every local is defined once; `Output{...}` is
built once.

```go
out := Output{InvoiceNumber: domain.InvoiceNumberFor(row.Seq), CustomerID: row.CustomerID}
```
English: `6. Build the answer:` / ``- `invoice_number` = INV- followed by the new invoice's `seq` as six digits``

Refused: `x = 3` - `refused: reassignment (=) is not in the allowed pattern list (Handle body)`

### S6

**postcondition** - `assert.Post(<cond>, "<reason>")` after the last S2-S5; at least one.

English: `7. Check that:` / ``- the answer's `customer_id` equals the request's `customer_id` ("invoice belongs to the requested customer")`` / `If any of these is false, it is a bug: stop with HTTP 500 Internal Server Error.`

Refused: a success return with none before it - `refused: success return without a postcondition before it is not in the allowed pattern list (S7 success return)`

### S7

**success return** - `return <name>, nil`, the last statement.

English: `8. Commit the transaction, then answer HTTP 201 Created with the answer. If the commit fails, stop with HTTP 500 Internal Server Error: nothing is written.`

Refused: `return Output{...}, nil` inside a loop - `refused: for loop is not in the allowed pattern list (Handle body)`

### S8

**map each** - `items := make([]domain.<T>, len(rows)); for i, row := range rows { items[i] = domain.<T>{...} }`,
only over a Q5 result.

English: `6. For each of the listed invoices, build an invoice summary:` ... `If there are no listed invoices, that list is empty.`

Refused: any other loop - `refused: for loop is not in the allowed pattern list (Handle body). Allowed here: S1 precondition, ...`

### S9

**next cursor** - `next := page.NextAfter(rows, "<cursor column>", <the Q5 LIMIT value>)`, at most once.

English: `7. Let the next cursor be the `seq` of the last listed invoice if the page is full (there are the request's `limit` listed invoices), otherwise 0: there is no next page.`

Refused: `page.NextAfter(rows, "seq", in.After)` - ``refused: page.NextAfter limit the request's `after`, which is not the page query's LIMIT (the request's `limit`)``

### S10

**claim check** - after a Q6 claim `n, err := a.q.<Claim>(...)`, an S2 guard
whose **entire condition** is `n != 1`, `if n != 1 { return Output{}, F<n> }`,
before the success return. `n` is compared only as `!= 1`, `== 1` or `== 0`.
The same test inside a compound condition (`&&`, `||`, `!`) does not count:
`claimed == 0 && claimed != 1` is `claimed == 0`, so a claim that changed 2
rows would pass it while the English still names "not exactly one". Compound
guards that mention the count may stay as extra guards (for example
`claimed == 0 && <a read that explains why>`), next to the check.

```go
claimed, err := a.q.ClaimSeat(ctx, db.ClaimSeatParams{Session: in.Session, Now: in.Now, ID: in.SeatID})
if err != nil {
	return Output{}, err
}
if claimed != 1 {
	return Output{}, F1
}
```
English: `3. If not exactly one seat was changed in step 2, stop with F1: HTTP 409 Conflict "seat is already held".` / `Any change made in step 2 is rolled back.`

**What a stop says about the claim's write.** A claim may change no row, so
"the write is rolled back" is only said where the claim did change one. Every
step that can stop the action (a guard, a query that fails, an assertion)
says, for each earlier write:

| Where | English |
|---|---|
| an insert (Q3) before it; or a claim, after a guard that stops unless exactly one row changed (`!= 1`), unless one row per entry of its list changed (S11, `!= int64(len(in.<List>))`) or unless one did (`== 0`) | `The write in step 2 is rolled back.` |
| a claim whose count is not known yet: a read right after it, or the `!= 1` (or S11) guard itself (0 rows, or several) | `Any change made in step 2 is rolled back.` |
| a guard whose condition includes `<n> == 0` (`claimed == 0 && ...`): it stops only when the claim changed nothing | `Nothing was written in step 2, so there is nothing to roll back.` |

The failure index says the same per F-ID: `before any write`, `after a write
that changed nothing, so nothing was written`, `after a write that may have
changed rows; any change it made is rolled back`, or `after a write, which is
rolled back`. From `adapter/testdata/good/release_example/release_example.en`:

```
3. Read: count the seats whose `id` is the request's `seat_id` (query `CountSeats` in queries/seat_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.
   Any change made in step 2 is rolled back.
4. If no seat was changed in step 2 and no seat has `id` equal to the request's `seat_id`, stop with F2: HTTP 404 Not Found "seat does not exist".
   Nothing was written in step 2, so there is nothing to roll back.
...
6. If not exactly one seat was changed in step 2, stop with F3: HTTP 409 Conflict "seat is not held by this session".
   Any change made in step 2 is rolled back.
...
8. Check that:
   - the found seat's `held_by` equals 0 and the found seat's `expires_at` equals 0 ("nobody holds the seat any more")
   If any of these is false, it is a bug: stop with HTTP 500 Internal Server Error.
   The write in step 2 is rolled back.
```

Refused:
- no `!= 1` guard - `refused: claim whose changed-row count no guard checks is not in the allowed pattern list (S10 claim check)`
- `claimed > 0` - `refused: comparison claimed > 0 on a claim's changed-row count is not in the allowed pattern list (S10 claim check)`
- only `if claimed == 0 && claimed != 1 { ... }` (`adapter/testdata/bad/claim_compound_check`) -
  `refused: claim whose changed-row count is checked only inside a compound condition (line 55) is not in the allowed pattern list (S10 claim check). The check is a guard of its own whose entire condition is claimed != 1: if claimed != 1 { return Output{}, F<n> }. Inside &&, || or ! it does not stop every wrong count (claimed == 0 && claimed != 1 stops only when no row changed, so a claim that changed too many rows passes); such guards may stay as extra guards`

### S11

**multi-row claim check** - after a Q6 claim over a Q7 IN list on the table's
key, `n, err := a.q.<Claim>(ctx, db.<Claim>Params{..., Ids: in.<List>})`, an
S2 guard whose **entire condition** is `n != int64(len(in.<List>))`, with the
same D10 list the claim's `IN (sqlc.slice(...))` was given, before the success
return. Every entry is at most one row (the key) and appears once (D10), so
the claim changed exactly one row per entry or the action stops and the
transaction rolls back every change: all rows or none. `n` is otherwise
compared only as `== 0`; `!= 1` and `== 1` are refused on a multi-row claim;
`len` appears nowhere else (not reversed, not without `int64`, not in a let).
Inside a compound condition the check does not count (as in S10). A stop says
about the claim's write what S10 says.

```go
confirmed, err := a.q.ConfirmTickets(ctx, db.ConfirmTicketsParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs})
if err != nil {
	return Output{}, err
}
...
if confirmed != int64(len(in.TicketIDs)) {
	return Output{}, F3
}
```
English (`adapter/testdata/good/confirm_many/confirm_many.en`):
```
5. If the number of tickets changed in step 2 is not the number of tickets in the request's `ticket_ids`, stop with F3: HTTP 409 Conflict "a ticket is not held by this session".
   Any change made in step 2 is rolled back.
6. Read: count the tickets whose `sold_to` is the session from the cookie and `id` is one of the request's `ticket_ids` (...). If the query fails, stop with HTTP 500 Internal Server Error.
   The write in step 2 is rolled back.
```

Refused (`adapter/testdata/bad/claim_many_checks`):
- `if confirmed != 1` - `refused: comparison confirmed != 1 on a multi-row claim's changed-row count is not in the allowed pattern list (S11 multi-row claim check). Compare the count of a claim over IN (sqlc.slice(...)) only as == 0 or in the check. After a claim over <key> IN (sqlc.slice(<name>)) (Q7), stop unless it changed one row per entry of the list: if confirmed != int64(len(in.TicketIDs)) { return Output{}, F<n> }`
- `if confirmed != int64(len(in.Codes))` - `refused: comparison with the length of in.Codes, which is not the list of the claim in step 1 (in.TicketIDs) is not in the allowed pattern list (S11 multi-row claim check). ...`
- only `if confirmed == 0 || confirmed != int64(len(in.TicketIDs))` - `refused: claim whose changed-row count is checked only inside a compound condition (line 50) is not in the allowed pattern list (S11 multi-row claim check). ...`
- `wanted := int64(len(in.TicketIDs))` - `refused: call to int64 is not in the allowed pattern list (expression). E5: only domain.<Func>(...) and page.IsPageLimit(...) calls; queries go through S3; len only as int64(len(in.<List>)) in the S11 check`
- `confirmed != int64(len(in.TicketIDs))` on a single-row claim - `refused: comparison confirmed != int64(len(in.TicketIDs)) on a single-row claim's changed-row count is not in the allowed pattern list (S11 multi-row claim check). ...`

---

## Expressions: E1-E7

### E1

**name** - `<local>` or `<local>.<Field>[.<Field>]`. No Go name reaches the
English: request and answer fields by JSON name, query results by table,
domain records by display name.

English: `in.CustomerID` -> ``the request's `customer_id` ``; `row.Seq` -> ``the new invoice's `seq` ``; `in.Now` (T1) -> `the current time`.

Refused: `page.MaxPageSize` used as a value in Handle - `refused: package-level value page.MaxPageSize is not in the allowed pattern list (expression). E5: call domain functions; no package-level values`

### E2

**literal** - integer, string, `true`, `false`, `nil`. English: `0`, `the text "INV-000000"`, `nothing`.

Refused: `1.5` - `refused: literal 1.5 is not in the allowed pattern list (expression). E2: integers and strings only`

### E3

**comparison** - `== != < <= > >=`. English: `equals`, `does not equal`,
`is less than`, `is at most`, `is greater than`, `is at least`. Against the
current time on the right (`x <= in.Now`, or a domain function's
`expiresAt <= now` called with `in.Now`), the Q6 boundary words instead:
`is no later than`, `is earlier than`, `is later than`, `is no earlier than`,
`is exactly`, `is not exactly` the current time.

Refused: none of its own; operands follow E1-E7.

### E4

**logic** - `&& || !`. English: `and`, `or`, `it is false that ...` (a
negated domain predicate renders its own negative: `is not one of`).

Refused: `-x`, `^x` - `refused: unary operator - is not in the allowed pattern list (expression). E4: only !`

### E5

**domain call** - `domain.<Func>(<value>...)`; the English is rendered from
the function body (M2). The one runtime call is `page.IsPageLimit(<value>)`
(`runtime/page`): `<value>` is between 1 and `page.MaxPageSize` (both included).

```go
if !domain.IsSupportedCurrency(in.Currency) {
	return Output{}, F3
}
if !page.IsPageLimit(in.Limit) {
	return Output{}, F2
}
```
English: `2. If the request's `currency` is not one of "ZAR", "USD" or "EUR", stop with F3: ...` and
`1. If the request's `limit` is not between 1 and 100 (both included), stop with F2: HTTP 400 Bad Request "page limit is out of range".`

Refused: `len(in.Currency)` - `refused: call to len is not in the allowed pattern list (expression). E5: only domain.<Func>(...) and page.IsPageLimit(...) calls; queries go through S3; len only as int64(len(in.<List>)) in the S11 check`; `in.AmountCents + 1` - `refused: arithmetic operator + is not in the allowed pattern list (expression). Do arithmetic in internal/domain and call it (E5)`

### E6

**record** - `<Type>{<Field>: <value>, ...}`, keyed, of type `Output`,
`domain.<T>` or `db.<Query>Params`.

English: ``an amount of money with `cents` = the new invoice's `amount_cents` and `currency` = the new invoice's `currency` ``

Refused: `struct{ X int }{1}` or a closure - `refused: function literal (closure) is not in the allowed pattern list (expression)`

### E7

**parentheses** - `(<expr>)`, rendered as parentheses.

---

## SQL: Q0-Q7

`queries/*.sql`, read by a strict shape parser. Anything outside a shape is
refused at `file:line:col`.

### Q0

**query** - `-- name: <Query> :one|:many|:execrows`, then exactly one
statement. `:one` is Q1-Q3, `:many` only Q5, `:execrows` only Q6.

Refused: `:many` on a non-page select - `refused: query annotation :many on a non-page shape is not in the allowed pattern list (Q0 query annotation)`

### Q1

**count** - `SELECT COUNT(*) FROM <table> WHERE <col> = <value> [AND ...]`.

English: `Read: count the customers whose `id` is the request's `customer_id` (...)`; in a guard `customers == 0` -> ``no customer has `id` equal to the request's `customer_id` ``

**Comparing with the current time.** A condition may also compare a column
with the server-set current time (T1), with or without an offset, exactly as
in a Q6 claim: `<col> <op> sqlc.arg(now) [+ or - <seconds>]`, where `<op>` is
`<>`, `<`, `<=`, `>` or `>=` (or `=` with an offset). The column comes first,
the parameter is never inside an OR, and a Q7 IN list still comes last. The
action must pass **exactly its clock input** for that parameter
(`Now: in.Now`, the Input field tagged `clock:"now"`), because the English
says "the current time": a request field, a let, a literal or any other value
is refused. It is said in the Q6 boundary words (see the
[Q6 table](#q6)), and a guard on such a count says "there is (no | at least
one) <row> whose ...", so the words stay exactly those of Q6.

```sql
-- name: CountStillHeld :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(now) AND id IN (sqlc.slice(ids));
```
```go
held, err := a.q.CountStillHeld(ctx, db.CountStillHeldParams{Session: in.Session, Now: in.Now, Ids: in.TicketIDs})
...
if held != 0 {
	return Output{}, F2
}
```
English (`adapter/testdata/good/confirm_many/confirm_many.en`):
```
3. Read: count the tickets whose `held_by` is the session from the cookie and `expires_at` is no later than the current time and `id` is one of the request's `ticket_ids` (query `CountStillHeld` in queries/tickets_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.
   Any change made in step 2 is rolled back.
4. If there is at least one ticket whose `held_by` is the session from the cookie and `expires_at` is no later than the current time and `id` is one of the request's `ticket_ids`, stop with F2: HTTP 410 Gone "a hold has expired".
```
A ticket whose `expires_at` is exactly the current time is counted (the
checks prove it on SQLite at the boundary second, and one second earlier it
is not). A plain `<col> = sqlc.arg(now)` stays a Q4 equality:
`` `expires_at` is the current time ``.

Refused:
- `... WHERE id = ? OR name = ?` - `refused: OR in WHERE is not in the allowed pattern list (query CustomerByIDOrName)`
- `expires_at <= sqlc.arg(cutoff)` with `Cutoff: in.Cutoff` (`adapter/testdata/bad/read_clock_source`) - `refused: comparison expires_at <= sqlc.arg(cutoff) in query CountHeldUntil, whose value in.Cutoff is not the server-set current time is not in the allowed pattern list (Q1 count). In a Q1 count or Q2 one-row read, a column is compared with <> < <= > >= or with an offset only against the action's server-set current time (T1): <col> <op> sqlc.arg(now) [+ or - <seconds>], the column first, with the action passing in.Now (an Input field tagged clock:"now") for it; every other condition is <col> = <value>`
- `expires_at <= 1800000000` - `refused: comparison expires_at <= 1800000000 in a read is not in the allowed pattern list (query CountEndedBefore). In a Q1 count or Q2 one-row read, ...`
- `sqlc.arg(now) >= expires_at` (`adapter/testdata/bad/read_clock_position`) - `refused: parameter on the left of a condition is not in the allowed pattern list (query CountClockLeft). In a Q1 count or Q2 one-row read, ...`
- `id IN (sqlc.slice(ids)) AND expires_at <= sqlc.arg(now)` - `refused: parameter now after IN (sqlc.slice(ids)) is not in the allowed pattern list (query CountAfterList). sqlc numbers the parameters as if the slice were one value, ...` (Q7)

### Q2

**one row** - `SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND ...]`.
A condition may compare a column with the server-set current time exactly as
in Q1 (`<col> <op> sqlc.arg(now) [+ or - <seconds>]`, bound to `in.Now`
itself), said in the Q6 boundary words.

```sql
-- name: SeatHolder :one
SELECT held_by, expires_at FROM seats WHERE id = sqlc.arg(id) AND expires_at > sqlc.arg(now);
```
```go
seat, err := a.q.SeatHolder(ctx, db.SeatHolderParams{ID: in.SeatID, Now: in.Now})
```
English: `Read: find a seat whose `id` is the request's `seat_id` and `expires_at` is later than the current time (query `SeatHolder` in queries/seat_after.sql); if several match, the first row returned is used. Call it the found seat.` / `If no seat matches or the query fails, stop with HTTP 500 Internal Server Error.` (`TestClockComparisonInReads`)

Refused: `SELECT * ...` - `refused: SELECT * is not in the allowed pattern list`; `... JOIN ...` - `refused: JOIN is not in the allowed pattern list`; `expires_at > sqlc.arg(now) - 600` with `Now: in.Cutoff` (`adapter/testdata/bad/read_clock_source`) - `refused: comparison expires_at > sqlc.arg(now) - 600 in query FindLiveHold, whose value in.Cutoff is not the server-set current time is not in the allowed pattern list (Q2 one row). ...`

### Q3

**insert** - `INSERT INTO <table> (<col>, ...) VALUES (<value>, ...) RETURNING <col>, ...`;
a value may be `(SELECT COALESCE(MAX(<col>), 0) + 1 FROM <table>)` for the same column and table.

English: ``5. Write: add one invoice to table `invoices` with `seq` = one more than the largest `seq` in `invoices` (1 if there is none), `customer_id` = the request's `customer_id`, ... Call the stored row the new invoice.``

Refused: `INSERT OR IGNORE ...` - `refused: INSERT OR IGNORE is not in the allowed pattern list (query InsertInvoice). Expected INTO after INSERT`; `ON CONFLICT` - `refused: ON CONFLICT (upsert) is not in the allowed pattern list`

### Q4

**value** - `?` or `sqlc.arg(<name>)` (a parameter), an integer, or `'text'`.

Refused: `WHERE id = (SELECT 1)` - `refused: subquery is not in the allowed pattern list`

### Q5

**keyset page** - `SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND ...] AND <cursor> < <value> ORDER BY <cursor> DESC LIMIT <n>`;
`:many` only, GET slices only, no OFFSET; the action guards its limit with `page.IsPageLimit`
(`runtime/page`, with `page.MaxPageSize` 100 and `page.DefaultPageSize` 20).

English: `5. Read: list the invoices whose `customer_id` is the request's `customer_id` and whose `seq` is less than the request's `after`, highest `seq` first, at most the request's `limit` of them (...). Call them the listed invoices; there may be none.`

Refused: `... LIMIT ? OFFSET ?` - `refused: OFFSET ...`; a write query in a GET - `refused: write query AddInvoice in a GET action`; a comparison with the current time in a page (`adapter/testdata/bad/read_clock_position`) - `refused: comparison expires_at <= sqlc.arg(now) in a keyset page is not in the allowed pattern list (query PageEnded). A Q5 keyset page compares only its cursor (<cursor> < <value>); every other condition is <col> = <value>. A comparison with the current time belongs in a Q1 count, a Q2 one-row read or a Q6 claim`

### Q6

**claim update** - one statement that checks and writes together:

```sql
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600
WHERE id = sqlc.arg(id) AND (held_by = 0 OR expires_at <= sqlc.arg(now));
```

The holder is the server-set session (T2), never an id from the request
body, and the hold is stored as the time it ends (`expires_at`), so every
read and claim compares that column with `now` directly.

- `SET <col> = <value>, ...`; `WHERE <cond> [AND <cond>]...`, where `<cond>` is
  `<col> = <> < <= > >= <value>`, or one parenthesised `(<cond> OR <cond> ...)` group;
- at least one `<col> = <parameter>` outside the group (which rows);
- a parameter may be plus or minus a whole number (`sqlc.arg(now) + 600`);
- `:execrows` only, no `RETURNING`; the action checks the count (S10, or S11
  for a claim over a Q7 IN list).

English: ``2. Claim: in table `seats`, set `held_by` = the session from the cookie and `expires_at` = 10 minutes after the current time on each seat whose `id` is the request's `seat_id` and (`held_by` is 0 or `expires_at` is no later than the current time) at that moment (query `ClaimSeat` in queries/claim_seat.sql). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same seat.``

**Comparisons with the current time.** Next to the current time (T1) a
comparison is said as a distance, and every phrase names its boundary: a
time exactly `d` away is inside "`d` or more", "no earlier than" and "no
later than", outside "more than `d`", "later than" and "earlier than".
With no offset the same words compare with the current time itself: an
`expires_at` equal to the current time is "no later than" and "no earlier
than" it, and neither "earlier than" nor "later than" it.
`TestClockComparisonWording`, `TestClockComparisonTable` and
`TestClockComparisonWithoutOffset` pin these lines, and
`TestClockComparisonInReads` pins the same words in a Q1 count and a Q2
one-row read (where the value must be the clock input `in.Now` itself);
the claim checks prove the claim's `expires_at <= sqlc.arg(now)` against
SQLite (a hold that expires one second after now blocks the seat, one that
expires exactly now does not), and the confirm_many checks prove the read's `expires_at <=
sqlc.arg(now)` at the boundary second (a hold ending exactly now is counted,
one second earlier it is not).

| SQL | English |
|---|---|
| `seen_at <= sqlc.arg(now) - 600` | `` `seen_at` is 10 minutes or more before the current time `` |
| `seen_at < sqlc.arg(now) - 600` | `` `seen_at` is more than 10 minutes before the current time `` |
| `seen_at > sqlc.arg(now) - 600` | `` `seen_at` is later than 10 minutes before the current time `` |
| `seen_at >= sqlc.arg(now) - 600` | `` `seen_at` is no earlier than 10 minutes before the current time `` |
| `seen_at = sqlc.arg(now) - 600` / `<>` | `` `seen_at` is exactly 10 minutes before the current time `` / `is not exactly` |
| `ends_at >= sqlc.arg(now) + 600` | `` `ends_at` is 10 minutes or more after the current time `` |
| `ends_at > sqlc.arg(now) + 600` | `` `ends_at` is more than 10 minutes after the current time `` |
| `ends_at < sqlc.arg(now) + 600` | `` `ends_at` is earlier than 10 minutes after the current time `` |
| `ends_at <= sqlc.arg(now) + 600` | `` `ends_at` is no later than 10 minutes after the current time `` |
| `expires_at <= sqlc.arg(now)` | `` `expires_at` is no later than the current time `` |
| `expires_at < sqlc.arg(now)` | `` `expires_at` is earlier than the current time `` |
| `expires_at > sqlc.arg(now)` | `` `expires_at` is later than the current time `` |
| `expires_at >= sqlc.arg(now)` | `` `expires_at` is no earlier than the current time `` |
| `expires_at = sqlc.arg(now)` / `<>` | `` `expires_at` is exactly the current time `` / `is not exactly` |

Refused:
- `UPDATE seats SET held_by = ?;` - `refused: end of statement ... Expected WHERE after SET (a claim names its rows and its condition; an UPDATE without WHERE changes every row)`
- `... RETURNING id` - `refused: RETURNING on an UPDATE is not in the allowed pattern list (query ClaimSeat)`
- `-- name: ClaimSeat :one` - `refused: query annotation :one on a claim update is not in the allowed pattern list (Q0 query annotation)`
- `WHERE held_by = 0 OR expires_at <= ...` (no parentheses) - `refused: OR in WHERE is not in the allowed pattern list (query ClaimSeat)`
- `SET expires_at = ? WHERE id = ? AND expires_at <= ?` - `refused: second bare ? for column expires_at ... name this one with sqlc.arg(<name>)`

### Q7

**IN list** - `<col> IN (sqlc.slice(<name>))`, one AND condition of a Q1, Q2
or Q6 `WHERE`:

- never inside an OR group, at most one per query, never a literal list or a
  subquery, and `sqlc.slice` nowhere else (not in `SET`, `VALUES` or `=`);
- it is the **last parameter of the statement**: sqlc numbers the parameters
  of a statement as if the slice were one value (`?1`, `?2`, ...) and expands
  it into one `?` per entry when the query runs; SQLite gives each of those
  `?` the next number, so a numbered parameter after the slice would be bound
  to one of the list's entries instead of its own value (with modernc.org/sqlite,
  `expires_at > ?3` after a 3-entry list compares with the second id, and an
  expired hold is sold). Parameters before the slice keep their numbers;
- the action passes exactly a D10 list input for it (`Ids: in.TicketIDs`);
- in a Q6 claim, `<col>` is the table's single-column `PRIMARY KEY` in
  `schema.sql`, so each entry is at most one row and S11 can compare the
  number of rows changed with the number of entries.

```sql
-- name: ConfirmTickets :execrows
UPDATE tickets
SET sold_to = sqlc.arg(session), held_by = ''
WHERE held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now) AND id IN (sqlc.slice(ids));
```
English: ``2. Claim: in table `tickets`, set `sold_to` = the session from the cookie and `held_by` = the text "" on each ticket whose `held_by` is the session from the cookie and `expires_at` is later than the current time and `id` is one of the request's `ticket_ids` at that moment (...)``; in a read, ``count the tickets whose `sold_to` is the session from the cookie and `id` is one of the request's `ticket_ids` ``; in a guard on a count, ``at least one ticket has `held_by` equal to the session from the cookie and `id` equal to one of the request's `ticket_ids` `` (or, when the count also compares with the current time, Q1, ``there is at least one ticket whose `held_by` is the session from the cookie and `expires_at` is no later than the current time and `id` is one of the request's `ticket_ids` ``).

With a comparison with the current time in the same read (Q1), the slice still
comes last: `held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(now) AND id IN (sqlc.slice(ids))`
numbers `session` ?1 and `now` ?2 before the list, which the confirm_many
checks run on SQLite (`TestExpiredReadBoundary`).

Refused (`adapter/testdata/bad/list_shapes`):
- `WHERE id IN (sqlc.slice(ids)) AND held_by = sqlc.arg(session) ...` - `refused: parameter session after IN (sqlc.slice(ids)) is not in the allowed pattern list (query ConfirmSliceFirst). sqlc numbers the parameters as if the slice were one value, so with SQLite a parameter after it is bound to one of the list's entries instead of its own value; put id IN (sqlc.slice(ids)) last in the statement`
- a claim over `held_by IN (sqlc.slice(holders))` - `refused: claim over IN (sqlc.slice(holders)) on column held_by, which schema.sql does not declare as the single-column PRIMARY KEY of table tickets is not in the allowed pattern list (query ConfirmByHolder). ...`
- `(expires_at = 0 OR id IN (sqlc.slice(ids)))` - `refused: IN inside an OR group is not in the allowed pattern list (query ConfirmInGroup). ...`
- `WHERE id IN (1, 2)` - `refused: SQL "1" is not in the allowed pattern list (query CountListed). Expected (sqlc.slice(<name>)) after IN ...`
- `a.q.CountTickets(ctx, in.Scalar)` - `refused: value in.Scalar for IN (sqlc.slice(ids)) that is not a list input is not in the allowed pattern list (Q7 IN list). ...`
- `... id IN (sqlc.slice(ids)) AND expires_at <= sqlc.arg(now)` in a read (`adapter/testdata/bad/read_clock_position`) - `refused: parameter now after IN (sqlc.slice(ids)) is not in the allowed pattern list (query CountAfterList). ...`

---

## Rules across statements: T1-T4, W1

T1-T4 bind the request: the values the server sets and the caller must not
send (the time T1, the session T2, the signed-in user and role T3), and the
query string a GET accepts (T4). W1 orders the queries of one action.

### T1

**time is passed in** - logic never reads the clock. `action.go` and
`internal/domain` never import `time`. The current time is an Input field:

```go
type Input struct {
	SeatID int64 `json:"seat_id"`
	Now    int64 `json:"now" clock:"now"`
}
```

`httpx.Bind` sets it from the server clock (unix seconds) and refuses a
request that sends it, in the body or the query string, in any letter case;
checks call `Handle` with any `Now` they like, so
"10 minutes later" is a test input, not a sleep. Next to the current time,
a Q6 offset in seconds is said in minutes, hours or days.

English:
```
The action also takes this value, which the caller does not send:
- `now`: set by the server to the current time when the request arrives, in whole seconds since 1970-01-01 UTC; the caller does not send it, and a request that does is answered with HTTP 400 below.
```
and in steps: `` `expires_at` is no later than the current time `` (see Q6 for every operator; the same words in a Q1 or Q2 read, whose parameter must be bound to `in.Now` itself).

Refused:
- `import "time"` - `refused: import "time" is not in the allowed pattern list (T1 time is passed in). Logic never reads the clock. ...`
- `now := time.Now().Unix()` - `refused: clock read time.Now().Unix is not in the allowed pattern list (T1 time is passed in)`
- `Now string \`json:"now" clock:"now"\`` - `refused: clock field Now that is not int64 tagged clock:"now" ...`

### T2

**session is passed in** - the caller's identity is a server-set input, like
the time. At most one Input field, `string` or `int64`, tagged
`server:"session"`:

```go
type Input struct {
	SeatID  int64  `json:"seat_id"`
	Session string `json:"session" server:"session"`
}
```

Who the caller is comes only from this field (or, for a signed-in user,
from the T3 fields). Never take an identity from
the request body (`person_id`, `user_id`, `owner_id`): the caller can send
any value, so a claim or a check written with it lets anyone act in someone
else's name. A text session's guard is `if in.Session == "" { return
Output{}, F<n> }`; an `int64` session's is `if in.Session <= 0 { ... }`.

`httpx.Bind` (`runtime/httpx`) sets it from the cookie
`httpx.SessionCookie`, which is always `bridge_session`:

- exactly one `bridge_session` cookie whose value is a whole number from 1
  up, in digits only (`int64`), or 1 to 128 letters, digits, `-`, `_` or `.`
  (`string`): the field is that value;
- no such cookie, more than one, or any other value: the field is the
  empty text (0 for `int64`), and the action runs, so its own guard raises its failure
  (`if in.Session == "" { return Output{}, F1 }`, or `<= 0` for `int64`);
- a request whose body or query string sends `session`, in any letter case,
  is answered with HTTP 400 `bad_request` and the action does not run.

Bind only reads the cookie: it never mints, signs or renews it and checks it
against no store; whoever presents the value is that session. The app issues
it (for example when it serves its web page) with an unguessable value,
`HttpOnly`, `SameSite=Lax` and, over HTTPS, `Secure`. Checks call `Handle`
with any session they like.

English for an `int64` session (`adapter/testdata/good/release_example/release_example.en`;
a `string` session, as in `confirm_many.en`, says `its value when that is 1
to 128 letters, digits, '-', '_' or '.', or the empty text when ...` and, in
steps, `If the session from the cookie equals the text ""`), apart from the
fields the caller sends:
```
The request body is one JSON object with this field and no others:
- `seat_id`: a whole number.
...
The action also takes this value, which the caller does not send:
- `session`: set by the server from the session cookie `bridge_session`: its value when that is a whole number from 1 up, written in digits only, or 0 when the request has no such cookie, has it more than once, or its value is anything else; the caller does not send it, and a request that does is answered with HTTP 400 below.
```
the 400 answer adds `; or the request sends a value that the server sets, in
the body or in the query string`, and in steps it is `the session from the
cookie`: ``1. If the session from the cookie is at most 0, stop with F1: HTTP 401 Unauthorized "session is required".``

Refused:
- `Session int64 \`json:"session" server:"admin"\`` - `refused: server field Session tagged server:"admin" is not in the allowed pattern list (T2 session is passed in)`
- `Session bool \`json:"session" server:"session"\`` - `refused: server field Session of type bool ...`
- `Session int64 \`json:"session" query:"session" server:"session"\`` - `refused: server field Session with a path or query tag ...`
- a `server` tag on Output - `refused: server tag on SeatID outside Input ...`
- a second `server:"session"` field - `refused: second session field Other ...`

### T3

**signed-in user and role are passed in** - sign-in, password hashing and
sessions stay in the app; bridge-en only takes **who is signed in** and
**their role**, as server-set inputs like the time and the session. At most
one Input field `int64` or `string` tagged `server:"user"`, named `user`,
and at most one `string` field tagged `server:"role"`, named `role`:

```go
type Input struct {
	Title    string `json:"title"`
	StartsAt int64  `json:"starts_at"`
	User     int64  `json:"user" server:"user"`
	Role     string `json:"role" server:"role"`
	Now      int64  `json:"now" clock:"now"`
}
```

`httpx.Bind` (`runtime/httpx`) fills them from the app's sign-in hook
(`httpx.Identity`, installed with `httpx.Identify`, A3), never from the
request:

- signed in (the hook says so, the user id is 1 to 128 letters, digits,
  `-`, `_` or `.` (a whole number from 1 up for an `int64` field) and the
  role is one of the app's `httpx.AppRoles`, A2): the user's id and role;
- otherwise nobody is signed in: an action declared with `Roles` (A1) never
  runs (HTTP 401), and a `Public` one gets 0 (or the empty text) and the
  empty text;
- a request whose body or query string sends `user` or `role`, in any letter
  case, is answered with HTTP 400 `bad_request` and the action does not run.

Who the caller is comes only from these fields (or the T2 session). An
Input field the caller sends may not be named `user`, `role`, `user_id` or
`role_id`; a field about someone else takes another name (`member_id`).
Checks call `Handle` with any user and role they like.

English (`adapter/testdata/good/create_event/create_event.en`, quoting
`httpx.UserRule` and `httpx.RoleRule`), apart from the fields the caller
sends:
```
The action also takes these 3 values, which the caller does not send:
- `user`: set by the server: the signed-in user (from the app's sign-in session); the caller does not send it, and a request that does is answered with HTTP 400 below.
- `role`: set by the server: the signed-in user's role (from the app's sign-in session); the caller does not send it, and a request that does is answered with HTTP 400 below.
```
For a `Public` action (`adapter/testdata/good/my_events/my_events.en`,
`httpx.SignedOutRule`): `` `user`: set by the server: the signed-in user
(from the app's sign-in session), or 0 when the caller is not signed in; ...``
In steps they are `the signed-in user` and `the signed-in user's role`:
``3. Write: add one event to table `events` with `organizer_id` = the signed-in user, `created_as` = the signed-in user's role, ...``

Refused:
- `UserID int64 \`json:"user_id"\``, `Role string \`json:"role"\`` (`adapter/testdata/bad/identity_from_body`) -
  `refused: field UserID with json name "user_id" that the caller sends is not in the allowed pattern list (T3 user, role passed in). Who the caller is never comes from the request. The server sets who the caller is: User int64 \`json:"user" server:"user"\` (or string) is the signed-in user and Role string \`json:"role" server:"role"\` their role; httpx.Bind fills both from the app's sign-in hook (httpx.Identify), and a request that sends them is HTTP 400. A field about someone else takes another name (for example member_id)`
- `User int64 \`json:"owner" server:"user"\`` - `refused: server:"user" field User with json name "owner" is not in the allowed pattern list (T3 user, role passed in). Its json name is "user", ...`
- `Role int64 \`json:"role" server:"role"\`` - `refused: server field Role of type int64 ...`; a path or query tag - `refused: server field User with a path or query tag ...`; a second `server:"user"` field - `refused: second user field Other ...`; on Output - `refused: server tag on CreatedAs outside Input ...` (`TestRolesRefusals`)

### T4

**strict query string** - a GET request's query string takes only the values
its Input declares with `query:"<name>"`, as strictly as a JSON body takes
only its listed fields. `httpx.Bind` (`runtime/httpx`) answers HTTP 400
`bad_request`, and the action does not run, when a GET sends any other query
parameter: an unknown name (`?debug=1`, `?offset=10`, a cache-buster
`?_=123`), a declared name in another letter case (`?Limit=5`), or a path
value's name (`?id=2`). A server-set name (`now`, `session`, `user`,
`role`, T1-T3) is HTTP 400 too, in any letter case (with its own message).
A GET that declares no query value (`my_events`) refuses every query
parameter. Before 0.4.0 an unknown query parameter was ignored, so a typo
(`?limt=5`) silently gave the default page.

```go
type Input struct {
	CustomerID int64 `json:"customer_id" path:"id"`
	After      int64 `json:"after" query:"after"`
	Limit      int64 `json:"limit" query:"limit"`
}
```
English (`adapter/testdata/good/list_customer_invoices/list_customer_invoices.en`,
quoting `httpx.StrictQueryRule` after the GET inputs, and
`httpx.BadQueryWhen` in the 400 answer):
```
The query string is as strict as a body: a query parameter that is not listed above (names are case-sensitive) is answered with HTTP 400 below and the action does not run.
...
- HTTP 400 Bad Request, id "bad_request", with a message describing the problem, if a path value is missing, a value is not a whole number, a query value appears more than once, or the query string has a parameter not listed above (names are case-sensitive). The action does not run.
```
`runtime/httpx` `TestStrictQueryRule` proves each sentence; the
`list_customer_invoices` and `my_events` checks prove it over HTTP against
SQLite (`?debug=1`, `?Limit=2`, `?id=2`, `?x=1`: HTTP 400; the declared
`?limit=2&after=3`: HTTP 200).

Refused: nothing in `action.go` (the rule is the runtime's answer, said in
the English of every GET). At runtime: `GET /customers/1/invoices?offset=1` -
HTTP 400 `{"error": {"id": "bad_request", "message": "query parameter \"offset\" is not one this action takes"}}`.

### W1

**no check-then-write** - a Q3 or Q6 write to a table that an earlier Q1, Q2
or Q5 query of the same action read is refused. Reading and then writing lets
another call change the row in between; put the condition into the write
(Q6) and check the count (S10). Reading **after** a claim, to explain why it
changed nothing, is allowed.

Refused (`adapter/testdata/bad/check_then_write`):
```go
holds, err := a.q.SeatHolds(ctx, in.SeatID, in.Session) // SELECT COUNT(*) FROM seats ...
...
if holds != 0 {
	return Output{}, F1
}
claimed, err := a.q.ClaimSeat(ctx, ...)                  // UPDATE seats ...
```
`refused: write to table seats after reading it in step 2 (check-then-write) is not in the allowed pattern list (W1 no check-then-write). Do not read a row and then write it: another call can change it in between. Put the condition into the write itself ...`

Allowed (claim first, explain after):
```go
if claimed == 0 && holder.HeldBy == in.Session {   // holder read after the claim
	return Output{}, F2
}
```
English: ``If no seat was changed in step 2 and the found seat's `held_by` equals the session from the cookie, stop with F2 ...``

---

## Who may call it: A1-A4

Every action says who may call it, and the runtime enforces it **before**
`Handle` runs (A1-A3); which rows it may change in a table that belongs to
its users is declared in `schema.sql` and enforced by `-check` (A4). There is no default: an action that says nothing is refused
(deny by default). Sign-in itself (passwords, sessions) is the app's; the
app tells the runtime who is signed in through one hook.

### A1

**who may call it** - `action.go` declares, once, at the top level:

```go
var Roles = httpx.Roles("organizer", "admin") // signed-in users with one of these roles
```
or
```go
var Roles = httpx.Public // anyone, signed in or not
```

Roles are one or more distinct lowercase identifiers (`[a-z][a-z0-9_]*`, at
most 32 characters) given as string literals, each declared app-wide (A2).

English (quoting `httpx.RolesRule`, `adapter/testdata/good/create_event/create_event.en`),
right under the route, and the two answers in the contract:
```
Who may call it: signed-in users with role `organizer` or `admin`. Anyone else is answered with HTTP 403 (HTTP 401 if not signed in), and the action does not run: the server checks this before it reads the request.
...
- HTTP 401 Unauthorized, id "unauthorized", message "sign-in required", if the caller is not signed in. The action does not run.
- HTTP 403 Forbidden, id "forbidden", message "not allowed for this role", if the caller is signed in with a role not listed above. The action does not run.
```
or (`httpx.PublicRule`, `adapter/testdata/good/my_events/my_events.en`):
```
Who may call it: anyone, signed in or not.
```

Refused:
- no declaration (`adapter/testdata/bad/roles_missing`) -
  `testdata/bad/roles_missing/action.go:2:9: refused: action.go without a Roles declaration is not in the allowed pattern list (A1 who may call it). Every action declares who may call it, once, at the top level of action.go: var Roles = httpx.Roles("<role>", ...) (signed-in users with one of these roles; lowercase identifiers declared app-wide with httpx.AppRoles in cmd/server) or var Roles = httpx.Public (anyone, signed in or not); import github.com/pierre10101/go-ai-bridge/runtime/httpx for it`
- `httpx.Roles()` (`adapter/testdata/bad/roles_empty`) -
  `testdata/bad/roles_empty/action.go:16:5: refused: empty role list httpx.Roles() is not in the allowed pattern list (A1 who may call it). List at least one role, or declare var Roles = httpx.Public for an action anyone may call, signed in or not`
- other forms (`TestRolesRefusals`): `var Roles = []string{"admin"}`, a typed
  `var Roles httpx.Access = ...`, `httpx.Roles(names...)` - `refused: Roles declaration ... (A1 who may call it)`;
  `httpx.Roles("Admin")` - `refused: role "Admin" that is not a lowercase identifier ...`;
  `httpx.Roles(Route)` - `refused: role Route that is not a string literal ...`;
  `httpx.Roles("admin", "admin")` - `refused: role "admin" listed twice ...`;
  a second `Roles` - `refused: second Roles declaration ...`

### A2

**app roles** - the app declares every role a signed-in user can have,
once, in `cmd/server`, and hands it to `httpx.Identify` (A3):

```go
var AppRoles = httpx.AppRoles("customer", "organizer", "finance", "admin")
```

`bridge-en` reads that call (string literals, distinct lowercase
identifiers) and refuses an action that lists any other role, so a typo
(`"organiser"`) fails `-check` instead of locking everyone out. An app with
only `Public` actions needs no `AppRoles`. At runtime a role the sign-in
hook returns that `AppRoles` does not declare counts as not signed in.

Why app-wide: the role names are the one place where a typo is silent (a
misspelt role in `Roles` would answer 403 to every real user and pass
every check that uses the same misspelling). One list, read by bridge-en
and enforced by `httpx.Identify`, catches it at `-check` and costs one line.

Refused:
- an unknown role (`adapter/testdata/bad/roles_unknown`) -
  `testdata/bad/roles_unknown/action.go:16:25: refused: role "organiser" that cmd/server does not declare is not in the allowed pattern list (A2 app roles). The app's roles are declared once, in cmd/server/routes.go:27:16: "customer", "organizer", "finance" or "admin". Use one of them, or add it there`
- a role list in an app without `AppRoles` - `refused: role "admin" without an app-wide role list is not in the allowed pattern list (A2 app roles). Declare the app's roles once in cmd/server: var AppRoles = httpx.AppRoles("<role>", ...), and pass it to httpx.Identify`
- in `cmd/server` (`TestAppRoles`): `httpx.AppRoles()` - `refused: httpx.AppRoles without roles ...`; `"Admin"` - `refused: app role "Admin" that is not a lowercase identifier ...`; a role twice - `refused: app role "admin" listed twice ...`; a second call - `refused: second httpx.AppRoles call (the first is at cmd/server/routes.go:5:16) ...`

### A3

**enforced before Handle** - `cmd/server` binds every route with the
slice's own `Roles`, and serves the mux through the app's sign-in hook:

```go
// The app's hook: who is signed in, with which role. Its sessions and
// password hashing are the app's own code; bridge-en never sees them.
func signedIn(r *http.Request) (user, role string, ok bool) { ... }

func Routes(db *sql.DB, identity httpx.Identity) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(create_event.Route, httpx.Bind(create_event.Roles, create_event.New(eventdb.New(txn.DB(db))).Handle))
	...
	return httpx.Identify(AppRoles, identity, mux) // main: Routes(db, signedIn)
}
```

`httpx.Identify` calls the hook once per request and remembers the
signed-in user for `httpx.Bind` (a request cannot set it). `httpx.Bind`
then, **before it reads the request**:

- an action declared with `Roles` and nobody signed in: HTTP 401
  `httpx.Unauthenticated` (`unauthorized`, "sign-in required");
- signed in with a role the action does not list: HTTP 403
  `httpx.Forbidden` (`forbidden`, "not allowed for this role");
- in both cases `Handle` does not run, no transaction begins and nothing is
  written; the body is not even decoded (a bad body still gets 401/403).

Deny by default at runtime too: without `httpx.Identify` (or with a nil
hook) nobody is signed in, and the zero `httpx.Access` lets nobody in.
`runtime/httpx` `TestRolesRule`, `TestZeroAccessDeniesEveryone`,
`TestPublicRule` and `TestUserAndRoleAreNeverSent` prove each sentence; the
create_event, create_invoice and list_customer_invoices checks prove 401,
403 and the allowed roles over HTTP against SQLite, with nothing written on
401/403.

Refused: a route bound without its own Roles (`httpx.Bind(handle)`,
`httpx.Bind(httpx.Public, ...)`, another slice's Roles) -
`cmd/server: create_event.Route is not served with github.com/pierre10101/go-ai-bridge/runtime/httpx.Bind(create_event.Roles, ...) over queries built on github.com/pierre10101/go-ai-bridge/runtime/txn.DB (H1, A3); ...` (`TestBoundNeedsTxn`)

### A4

**ownership** - a table whose rows belong to a user declares its **owner
column** in `schema.sql`, once, with a comment attached to its `CREATE
TABLE`: on the comment lines right above it (no blank line in between), or
at the end of the `CREATE TABLE` line itself.

```sql
-- One row per event. organizer_id: the signed-in user who created it.
-- owner: organizer_id
CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY,
    organizer_id INTEGER NOT NULL CHECK (organizer_id > 0),
    title        TEXT    NOT NULL,
    ...
);
```

The owner column is one of the table's columns, `INTEGER` (for an `int64`
signed-in user, T3) or `TEXT` (for a `string` one).

**Who bypasses ownership** is declared once, app-wide, chained on the A2
role list in `cmd/server`:

```go
var AppRoles = httpx.AppRoles("customer", "organizer", "finance", "admin").BypassOwnership("admin")
```

Each role it lists is one `AppRoles` declares (string literals, each once).
It is a declaration for `bridge-en`, not a runtime check: who may call an
action is still its `Roles` (A1, enforced by `httpx.Bind`), and ownership is
enforced by the SQL that `-check` accepts. Why this form: the one place that
lists the app's roles also says which of them are administrators, so a
reviewer sees every bypass in one line, and an action cannot grant itself
the bypass (its `Roles` can only name roles; the marker lives in
`cmd/server`).

**The rule.** An action that is `Public`, or whose `Roles` lists **any**
role without the bypass (for example `httpx.Roles("organizer", "admin")`),
writes an owned table only in the signed-in user's name:

- a Q6 UPDATE (claim) has `<owner col> = sqlc.arg(<p>)` as an AND
  condition of its `WHERE` (not inside an OR group) and does not `SET` the
  owner column to anything else (no giving a row away);
- a Q3 INSERT sets `<owner col> = sqlc.arg(<p>)`;
- for `<p>` the action passes exactly its signed-in user, the Input field
  tagged `server:"user"` (`OrganizerID: in.User`), whose type is the
  column's. A request field (`organizer_id` the caller sends), a literal,
  a let or a missing condition is refused;
- a `Public` action never writes an owned table (signed out, its user is 0
  or the empty text, which owns nothing).

An action whose `Roles` lists **only** roles with the bypass (here
`httpx.Roles("admin")`) may write any row.

**Reads are not refused.** An owned row is often meant to be read by others
(a public list of events, a customer reading an organizer's event), and a
read changes nothing, so refusing unscoped reads would block ordinary
features without protecting a write. Instead the English of every read of an
owned table says whether it is limited to the caller's own rows, so a
reviewer sees an unscoped read of private data in the `.en` diff.

```sql
-- name: RenameOwnEvent :execrows
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
```
```go
var Roles = httpx.Roles("organizer")
...
renamed, err := a.q.RenameOwnEvent(ctx, db.RenameOwnEventParams{Title: in.Title, ID: in.EventID, OrganizerID: in.User})
```

English (`adapter/testdata/good/rename_event/rename_event.en`), after the
claim sentence of the step:
```
2. Claim: in table `events`, set `title` = the request's `title` on each event whose `id` is the request's `event_id` and `organizer_id` is the signed-in user at that moment (...). ... Ownership: only events you own (`organizer_id` is the signed-in user) can be changed by this step. If the query fails, stop with HTTP 500 Internal Server Error.
```
and, for the other steps on an owned table:

| Step | English |
|---|---|
| insert setting the owner to the user (`create_event.en`) | `` Ownership: the new event is yours (`organizer_id` is the signed-in user). `` |
| read limited to the user (`my_events.en`) | `` Ownership: only events you own (`organizer_id` is the signed-in user) are read. `` |
| read not limited to the user | `` Ownership: this read is not limited to events you own (`organizer_id` is not compared with the signed-in user). `` |
| write by an admin-only action (`admin_rename_event.en`) | `` Ownership: this step is not limited to events you own (`organizer_id` need not be the signed-in user), because only role `admin` may call this action and cmd/server declares that it bypasses ownership. `` |

The checks of `rename_event` prove it over HTTP against SQLite: organizer B
renaming organizer A's event gets F2 (HTTP 404) and nothing is written,
also when B sends A's id as `user` (HTTP 400); A renames it. The checks of
`admin_rename_event` prove the admin renames anyone's event and an organizer
gets HTTP 403.

Refused (`file:line:col`, each with the fix: put `<col> = sqlc.arg(<col>)`
in the WHERE or the INSERT and pass `in.User`, or restrict the action to
roles that bypass ownership):
- an UPDATE with no owner condition, from an action an organizer may call (`adapter/testdata/bad/owner_unscoped`) -
  `testdata/bad/owner_unscoped/action.go:39:2: refused: write to owned table events (query RenameEvent) whose WHERE does not limit it to rows the signed-in user owns (organizer_id = the signed-in user) is not in the allowed pattern list (A4 ownership). Table events is owned by organizer_id (schema.sql:42:1), so an action that a role without the ownership bypass may call writes only rows the signed-in user owns: an UPDATE has organizer_id = sqlc.arg(organizer_id) as an AND condition of its WHERE (not inside an OR group) and sets organizer_id to nothing else, and an INSERT sets organizer_id = sqlc.arg(organizer_id); the action passes exactly the signed-in user for it (User int64 \`json:"user" server:"user"\`, then OrganizerID: in.User). A request field never counts, and a Public action never writes an owned table. If only administrators may do this, declare Roles with roles that bypass ownership (in cmd/server: httpx.AppRoles(...).BypassOwnership("admin"))`
- the owner scoped to a field the caller sends, in an UPDATE and an INSERT (`adapter/testdata/bad/owner_from_body`) -
  `testdata/bad/owner_from_body/action.go:41:106: refused: write to owned table events (query RenameEvent) whose owner column organizer_id is the request's \`organizer_id\`, which is not the signed-in user is not in the allowed pattern list (A4 ownership). ...` and
  `testdata/bad/owner_from_body/action.go:48:65: refused: write to owned table events (query CopyEvent) whose owner column organizer_id is the request's \`organizer_id\`, which is not the signed-in user ...`
- the annotation itself (`adapter/testdata/bad/owner_annotation`, a module of its own) -
  `schema.sql:4:1: refused: owner column organiser_id, which table events does not declare (its columns: id, organizer_id, title) is not in the allowed pattern list (A4 ownership). Declare a table's owner once, on a comment line right above its CREATE TABLE (no blank line in between): -- owner: <col>, where <col> is one of its columns of type INTEGER (an int64 signed-in user, server:"user") or TEXT (a string one)`;
  `schema.sql:12:1: refused: second owner annotation for table notes (the first is at schema.sql:11:1) ...`;
  `schema.sql:19:1: refused: owner column score of type "REAL", which is neither INTEGER nor TEXT ...`;
  `schema.sql:25:1: refused: owner annotation "-- owner: id, author" ...`;
  `schema.sql:31:1: refused: owner annotation "-- owner: author" that is not attached to a CREATE TABLE ...`;
  and a user field of another type than the owner column - `testdata/bad/owner_annotation/action.go:17:2: refused: signed-in user field User of type int64 for table notes, whose owner column author is TEXT (schema.sql:11:1) is not in the allowed pattern list (A4 ownership). The signed-in user is compared with the owner column, so they have the same type: User string \`json:"user" server:"user"\` for a TEXT owner column (or change the column's type in schema.sql)`
- other forms (`TestOwnershipRefusals`): a `Public` action - `refused: write to owned table events (query RenameOwnEvent) in a Public action ...`;
  no `server:"user"` field - `refused: write to owned table events (query RenameOwnEvent) in an action without the signed-in user ...`;
  the owner condition only inside an OR group, or `organizer_id = 7` - `... whose WHERE does not limit it ...`, `... whose owner column organizer_id is 7, which is not the signed-in user ...`;
  `SET organizer_id = sqlc.arg(new_owner)` - `refused: change of the owner column organizer_id of owned table events (query RenameOwnEvent) ...`;
  an INSERT without the owner column - `refused: insert into owned table events (query InsertEvent) that does not set its owner column organizer_id to the signed-in user ...`
- the bypass declaration (`TestBypassOwnership`): a role `AppRoles` does not declare - `cmd/server/routes.go:5:69: refused: ownership-bypass role "root" that httpx.AppRoles does not declare is not in the allowed pattern list (A4 ownership). ...`;
  a role twice, no role, a non-literal - `refused: ownership-bypass role "admin" listed twice ...`, `refused: BypassOwnership without roles ...`, `refused: ownership-bypass role admin that is not a string literal ...`;
  not chained on the `httpx.AppRoles(...)` call (`AppRoles.BypassOwnership("admin")` later, or twice) - `refused: AppRoles.BypassOwnership that is not chained on the httpx.AppRoles call ...`

---

## Outside the slice: M1-M3, H1

### M1

**domain type** - every `internal/domain` type an action uses has a
`// bridge-en: <display name>` doc line, a noun phrase without parentheses:
the only hand-written English.

```go
// bridge-en: an amount of money
type Money struct {
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"`
}
```
English: `an amount of money, as an object with `cents` (a whole number) and `currency` (text)`

Refused: `// bridge-en: an amount of money (whole cents and an ISO 4217 code)` - `refused: display name "..." of domain type Money with a description`

### M2

**domain function** - `assert.Pre/Post(<cond>, "<reason>")...` then
`return <expr>`; or for a bool, `switch <param> { case "<text>", ...: return true }`
then `return false`. The English comes from the body. No `bridge-en:`
comment on a function, no methods, no package-level variables; imports only
`fmt`, `github.com/pierre10101/go-ai-bridge/runtime/assert` and
`github.com/pierre10101/go-ai-bridge/runtime/shape`.

```go
func IsSupportedCurrency(code string) bool {
	switch code {
	case "ZAR", "USD", "EUR":
		return true
	}
	return false
}
```
English: `{code} is one of "ZAR", "USD" or "EUR"` (adding `"GBP"` changes exactly one English line).

Refused: a loop in a domain function - `refused: range loop is not in the allowed pattern list (domain function IsValidInvoiceNumber)`; `import "os"` - `refused: import "os" is not in the allowed pattern list (internal/domain). internal/domain is pure: it imports only fmt, github.com/pierre10101/go-ai-bridge/runtime/assert and github.com/pierre10101/go-ai-bridge/runtime/shape`

### M3

**domain expression** - parameters, literals, literal constants, conversions,
comparisons, logic, calls to M2 functions, `shape.Has(<text>, "<shape>")`
(`runtime/shape`; `#` is one digit) and `fmt.Sprintf` with `%d`, `%0<n>d`, `%s`.

English: `fmt.Sprintf("INV-%06d", seq)` -> `INV- followed by {seq} as six digits`; `shape.Has(s, "INV-######")` -> `{s} is INV- followed by six digits`

Refused: `seq+1` - `refused: arithmetic operator + is not in the allowed pattern list (domain function InvoiceNumberFor)`; `"INV-%x"` - `refused: format verb "%x" ...`

### H1

**http plumbing** - `github.com/pierre10101/go-ai-bridge/runtime/httpx` declares
`BadInput`, `Internal`, `SuccessStatus`, `InputRule`, `QueryInputRule`,
`BadQueryWhen`, `TxRule`, `ReadTxRule`, `ClockRule`, `SessionCookie`,
`SessionRule`, `SessionValue`, `ServerSetWhen`, `ListRule`, `ListRuleExact`,
`ListElems`, `ListWhen`, `PublicRule`, `RolesRule`, `Unauthenticated`,
`Forbidden`, `UserRule`, `RoleRule`, `SignedOutRule`, `SignedOutZero`,
`StrictQueryRule` and `ErrorBody`;
`bridge-en` quotes the values compiled into it, which are the app's because
`go.mod` pins the same version (the pin check above), and httpx's own tests
prove each one. `cmd/server` binds every route with the runtime's
`httpx.Bind(<slice>.Roles, ...)` (A3) over queries built on its `txn.DB`: one transaction per call
(`BEGIN IMMEDIATE` at the first query for writes, read-only for GET).

English: `Steps 3 to 7 run in one database transaction. It begins with the query in step 3 and holds the database's write lock until it ends, ...`

Refused: a route bound over a plain `*sql.DB`, or with an app's own `httpx` - `cmd/server: create_invoice.Route is not served with github.com/pierre10101/go-ai-bridge/runtime/httpx.Bind(create_invoice.Roles, ...) over queries built on github.com/pierre10101/go-ai-bridge/runtime/txn.DB (H1, A3); ...`

---

## Conditional claim and state-transition rules

For any action that moves a row from one state to another only if a
condition holds at that moment: claiming a resource, confirming or releasing
a hold, approving a request, consuming a one-time token. The seat hold in
`adapter/testdata/good/claim_example` is only an example.

1. **Pass the current time and the session (or signed-in user) in** (T1,
   T2, T3). No clock inside logic, and the caller never names who they are
   in the body: the holder or owner written by a claim is `in.Session`
   (`server:"session"`) or `in.User` (`server:"user"`), never a `person_id`
   or `user_id` input. In a table that declares its owner (A4), the claim
   also names the owner: `AND organizer_id = sqlc.arg(organizer_id)` with
   `OrganizerID: in.User`. Store when a hold ends (`SET expires_at =
   sqlc.arg(now) + 600`) and compare that column with the server-set `now`;
   the English names the boundary exactly (Q6 table): `expires_at <=
   sqlc.arg(now)` is "`expires_at` is no later than the current time", so a
   hold whose `expires_at` is exactly now has expired and one that expires a
   second later has not. Checks inject the time and test both sides of the
   boundary.
2. **Check and write in one statement** (Q6 + S10 + W1). One `UPDATE`
   changes the row only if it is in the expected state, and the action stops
   unless exactly one row changed. The English says the condition holds "at
   that moment" and "is checked by the same statement that writes".
   Check-then-write is refused.
3. **Write the failure cases before the code**, in `intent.md`, each in plain
   English with its exact boundary. For a transition, consider at least:
   - **not in the expected state** - for example the resource is held by
     someone else and the hold expires later than now;
   - **expired** - the hold, token or offer expires now or earlier;
   - **already done** - the same transition was applied before (confirmed
     twice, released twice);
   - **someone else's** - the row belongs to another session;
   - **no such row**.

   Each becomes an F-ID raised by a guard after the claim (`claimed == 0 && ...`
   reads that explain why nothing changed, allowed by W1), with the final
   `claimed != 1` guard as the catch-all (S10): a guard of its own, never
   part of a compound condition. The English of a `claimed == 0 && ...`
   guard says nothing was written (S10).
4. **Several rows at once: all or none** (D10 + Q7 + S11). Take the rows as
   a list input with bounds and no duplicates, name them in the claim with
   `<key> IN (sqlc.slice(<name>))` as the last parameter, and stop unless
   the claim changed one row per entry: `if n != int64(len(in.<List>))`. A
   stop rolls back every change, so one row that is not in the expected state
   (expired, someone else's, already done, missing) leaves all of them as
   they were. `adapter/testdata/good/confirm_many` confirms several held
   tickets in one call, then explains a stop with a read that compares with
   the current time (Q1: "`expires_at` is no later than the current time");
   its checks run against SQLite: all confirmed, one expired (nothing
   written, also at the boundary second), one not held by this session
   (everything rolled back), an empty or repeated list (400), and concurrent
   calls (the basket is sold once).

## Hard limits

- `Handle`: at most 70 lines. `action.go`: at most 300 lines. Input: at most 10 fields.
- List input (D10): at most `httpx.MaxListLen` (100) entries.
- Page size: 1..`page.MaxPageSize` (100); default `page.DefaultPageSize` (20), both in `github.com/pierre10101/go-ai-bridge/runtime/page`.
- No waivers: a refusal is fixed in the code, or by a new bridge-en release that adds a rule.

## The fixture app

The rulebook quotes a fixture app inside this repository,
`adapter/testdata` (module `example.com/fixtures`, which requires
`github.com/pierre10101/go-ai-bridge` with a `replace` to the checkout). It is
laid out like an app, except that its slices live in `good/` (rendered, with
golden `<slice>.en`) and `bad/` (refused, with `want.err`) instead of
`features/`. The go command ignores `testdata`, so `bridge-en` only reads it;
`scripts/smoke-app.sh` copies it into a new module, generates the `db/`
packages with sqlc and runs its checks for real. bridge-en itself ships no app.

## Agent instructions: bridge-en init

```sh
bridge-en init            # in the app's root; or: bridge-en init <dir>
bridge-en init -force     # overwrite the files below with this version's text
```

writes documents only, never code, into the directory:

| File | For | Content |
|---|---|---|
| `AGENTS.md` | every agent (the cross-tool standard) and people | the workflow: install by the go.mod pin, intent first, `-check` after every edit, `-write` and read the `.en` against the intent, never edit `.en`, the server's time, session and signed-in user and role passed in, every action's required `Roles` declaration, owned tables (A4) and strict GET queries (T4), claims, a thin UI, countdowns from the server's `now`/`expires_at`, errors on `error.id`, pull requests only, and an example feature |
| `.cursor/rules/bridge-en.mdc` | Cursor (`alwaysApply: true`) | "follow AGENTS.md" and the five rules that matter most |
| `CLAUDE.md` | Claude Code | the same pointer |
| `.github/copilot-instructions.md` | GitHub Copilot | the same pointer |
| `GEMINI.md` | Gemini CLI | the same pointer |

AGENTS.md is the single source of truth; the pointer files only send the
agent to it. An existing file is kept (`kept AGENTS.md (exists; bridge-en
init -force overwrites it)`) unless `-force`. The text is embedded in the
binary, so it is the text of the version installed, and the example feature
in AGENTS.md is tested to pass I1-I3 and render (`TestAgentsSkeletonRenders`).

## Review: the pull request comment

```sh
bridge-en pr-comment -base <base-sha> [-head HEAD] [-max 60000] features/*/
```

prints a markdown comment: for every feature directory with a change between
`<base>...<head>`, the files that changed, its `intent.md` (in full; its diff
when the pull request changes it) and the diff of its `<slice>.en` ("unchanged"
when the code changed but the English did not). A reviewer reads what was
asked next to what the code does. The comment starts with
`<!-- bridge-en:pr-english -->`, never exceeds `-max` characters (large
blocks are cut at a line with a note; features that do not fit are named),
and fences each block so its content cannot break the markdown.

The `pr-english` action posts it, updating its one comment in place:

```yaml
on: pull_request
jobs:
  english:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: "1.24.x" }
      - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.4.0
      - uses: pierre10101/go-ai-bridge/.github/actions/pr-english@v0.4.0
        # with:
        #   features: "features/*/"   # default
        #   max-chars: "60000"        # default
```

It writes the same text to the job summary. On a pull request from a fork,
GitHub gives the workflow a read-only token, so the action does not comment
(it says so in a notice); the job summary has the text. The action needs no
permission but `pull-requests: write` (and `contents: read` to check out).

