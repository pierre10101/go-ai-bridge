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
- [Declarations: D1-D9](#declarations-d1-d9)
- [Statements: S1-S10](#statements-s1-s10)
- [Expressions: E1-E7](#expressions-e1-e7)
- [SQL: Q0-Q6](#sql-q0-q6)
- [Rules across statements: T1, T2, W1](#rules-across-statements-t1-t2-w1)
- [Outside the slice: M1-M3, H1](#outside-the-slice-m1-m3-h1)
- [Conditional claim and state-transition rules](#conditional-claim-and-state-transition-rules)
- [Hard limits](#hard-limits)
- [The fixture app](#the-fixture-app)

## Install and pin

bridge-en is one Go module, `github.com/pierre10101/go-ai-bridge`, with two
parts an app uses:

- the **binary** `bridge-en` (`cmd/bridge-en`), which renders and checks slices;
- the **runtime** `github.com/pierre10101/go-ai-bridge/runtime/...`
  (`assert`, `failure`, `httpx`, `page`, `shape`, `store`, `txn`), which the
  app imports like any Go package. An app never copies bridge-en source.

```sh
# 1. In the app: depend on one version. go.mod is the pin.
go get github.com/pierre10101/go-ai-bridge@v0.1.2

# 2. Install the binary of the same version.
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.1.2
bridge-en -version                      # bridge-en 0.1.2
```

The app's `go.mod` then says:

```
require github.com/pierre10101/go-ai-bridge v0.1.2
```

In the app's CI, the `setup-bridge-en` action installs the binary of the
version `go.mod` requires (`go list -m github.com/pierre10101/go-ai-bridge`), or
the `version` you pass it:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: "1.24.x"
- uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.1.2   # version from go.mod
# or download the released binary and check its SHA256 instead of building it:
# - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.1.2
#   with: { method: release }
- run: bridge-en -check features/*/
```

Without GitHub Actions: `go install …@v<version>` as above, or download
`bridge-en_<version>_<os>_<arch>.tar.gz` and `SHA256SUMS` from the release and
check the hash.

**Why app pull requests cannot rewrite the English.** The English quotes the
runtime (for example `httpx.InputRule`, `httpx.TxRule`, `httpx.ClockRule`, `httpx.SessionRule`),
and the app runs that same runtime: both come from the one module version in
`go.mod`, verified by the Go checksum database. The app has no copy to edit.
`bridge-en -check` (and `-write`) first refuse an app whose `go.mod` requires
another version than the binary, or none:

```
go.mod: pins github.com/pierre10101/go-ai-bridge v0.0.9 but this is bridge-en v0.1.2; install the pinned version (go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.0.9) or move the app (go get github.com/pierre10101/go-ai-bridge@v0.1.2), then review every .en diff
```

To move an app to a new version: `go get github.com/pierre10101/go-ai-bridge@v<new>`,
install the same binary, run `bridge-en -write` on every slice, review the
diff of every `.en` file, commit.

## App layout

`bridge-en` reads an app from its module root (the directory with `go.mod`):

```
go.mod                       any module path; requires github.com/pierre10101/go-ai-bridge (the pin)
schema.sql / schema.go       the app's schema; schema.go embeds it for store.Open
sqlc.yaml                    one entry per slice
cmd/server/main.go           store.Open(ctx, path, schema), then serve Routes
cmd/server/routes.go         one line per slice: mux.Handle(<slice>.Route, httpx.Bind(...txn.DB(db)...))
features/<slice>/            one directory per action (below)
internal/domain/             the app's value objects and pure rules (M1-M3); may be absent
```

`httpx` and `txn` in `routes.go` are `github.com/pierre10101/go-ai-bridge/runtime/httpx`
and `.../runtime/txn`; `store` is `.../runtime/store`.

A slice:

```
features/<slice>/
  intent.md          why, inputs, outputs, failure cases F1..Fn  (write this FIRST)
  action.go          Route, Input, Output, F-IDs, Action, New, Handle (D1-D9, S1-S10)
  queries/*.sql      plain SQL with sqlc annotations (Q0-Q6)
  db/                sqlc-generated code (never edited by hand)
  checks/*_test.go   one TestF<n>_... per F-ID, referencing <slice>.F<n>
  <slice>.en         golden English (bridge-en -write), reviewed in the pull request
```

`bridge-en -check features/*/` also cross-checks: the F-IDs of `intent.md`,
`action.go` and `checks/` are the same set; every query in `queries/` is
called (no dead SQL); the route is bound with the runtime's `httpx.Bind` over
its `txn.DB`.

## Writing a slice

1. Write `intent.md` with every failure case (F1..Fn) in English **before any
   code**.
2. Write `queries/*.sql` (Q0-Q6) and run `sqlc generate`.
3. Write `action.go` inside D1-D9 / S1-S10.
4. Write one check per F-ID in `checks/` (`func TestF<n>_...` that references
   `<slice>.F<n>`).
5. Bind the route in `cmd/server/routes.go`.
6. `bridge-en -write features/<slice>`; read the `.en` like a reviewer would;
   commit it with the code.

---

## Declarations: D1-D9

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
`github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page}`,
`<module>/internal/domain` and `<module>/features/<slice>/db`; no renamed,
dot or blank imports.

```go
import (
	"context"
	"net/http"

	"example.com/app/features/create_invoice/db"
	"example.com/app/internal/domain"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
)
```
English: none (imports decide what can be said).

Refused: `import "fmt"` - `refused: import "fmt" is not in the allowed pattern list (D2 imports). Allowed: context, net/http, github.com/pierre10101/go-ai-bridge/runtime/{assert,failure,page}, <module>/internal/domain, <module>/features/hidden_magic/db`

### D3

**route** - `const Route = "<METHOD> /<path>"`, the only constant.

```go
const Route = "POST /invoices"
```
English: `The action "Create invoice" answers POST /invoices.`

Refused: `const MaxRetries = 3` - `refused: constant declaration MaxRetries is not in the allowed pattern list (D3 route). The only constant is Route = "<METHOD> /<path>"`

### D4

**input** - `type Input struct` with at most 10 fields, each `int64`, `string`,
`bool` or `domain.<T>`, each with a json tag. Every field is required (left
out or null is HTTP 400). Exception, the **server-set inputs**: an `int64`
field tagged `clock:"now"` (T1) and one `int64` or `string` field tagged
`server:"session"` (T2) are set by the server; the caller must not send them
(body or query string: HTTP 400), and the English lists them apart from the
fields the caller sends. GET fields take `path:"<name>"` or `query:"<name>"`.

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

Refused: `Items []int64 \`json:"items"\`` - `refused: field type []int64 is not in the allowed pattern list (D4 input). List fields are only on Output (D5); Input stays scalar`

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
the only package-level variables; every F-ID is raised by an S2 guard.
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

---

## Statements: S1-S10

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
`if n != 1 { return Output{}, F<n> }` before the success return. `n` is
compared only as `!= 1`, `== 1` or `== 0`.

```go
claimed, err := a.q.ClaimSeat(ctx, db.ClaimSeatParams{HeldBy: in.PersonID, Now: in.Now, ID: in.SeatID})
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
| an insert (Q3) before it; or a claim, after a guard that stops unless exactly one row changed (`!= 1`) or unless one did (`== 0`) | `The write in step 2 is rolled back.` |
| a claim whose count is not known yet: a read right after it, or the `!= 1` guard itself (0 rows, or several) | `Any change made in step 2 is rolled back.` |
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
   - the found seat's `held_by` equals 0 and the found seat's `held_at` equals 0 ("nobody holds the seat any more")
   If any of these is false, it is a bug: stop with HTTP 500 Internal Server Error.
   The write in step 2 is rolled back.
```

Refused: no `!= 1` guard - `refused: claim whose changed-row count no guard checks is not in the allowed pattern list (S10 claim check)`; `claimed > 0` - `refused: comparison claimed > 0 on a claim's changed-row count is not in the allowed pattern list (S10 claim check)`

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

Refused: `len(in.Currency)` - `refused: call to len is not in the allowed pattern list (expression). E5: only domain.<Func>(...) and page.IsPageLimit(...) calls; queries go through S3`; `in.AmountCents + 1` - `refused: arithmetic operator + is not in the allowed pattern list (expression). Do arithmetic in internal/domain and call it (E5)`

### E6

**record** - `<Type>{<Field>: <value>, ...}`, keyed, of type `Output`,
`domain.<T>` or `db.<Query>Params`.

English: ``an amount of money with `cents` = the new invoice's `amount_cents` and `currency` = the new invoice's `currency` ``

Refused: `struct{ X int }{1}` or a closure - `refused: function literal (closure) is not in the allowed pattern list (expression)`

### E7

**parentheses** - `(<expr>)`, rendered as parentheses.

---

## SQL: Q0-Q6

`queries/*.sql`, read by a strict shape parser. Anything outside a shape is
refused at `file:line:col`.

### Q0

**query** - `-- name: <Query> :one|:many|:execrows`, then exactly one
statement. `:one` is Q1-Q3, `:many` only Q5, `:execrows` only Q6.

Refused: `:many` on a non-page select - `refused: query annotation :many on a non-page shape is not in the allowed pattern list (Q0 query annotation)`

### Q1

**count** - `SELECT COUNT(*) FROM <table> WHERE <col> = <value> [AND ...]`.

English: `Read: count the customers whose `id` is the request's `customer_id` (...)`; in a guard `customers == 0` -> ``no customer has `id` equal to the request's `customer_id` ``

Refused: `... WHERE id = ? OR name = ?` - `refused: OR in WHERE is not in the allowed pattern list (query CustomerByIDOrName)`

### Q2

**one row** - `SELECT <col>, ... FROM <table> WHERE <col> = <value> [AND ...]`.

English: `Read: find a seat whose `id` is ... ; if several match, the first row returned is used. Call it the found seat.` / `If no seat matches or the query fails, stop with HTTP 500 Internal Server Error.`

Refused: `SELECT * ...` - `refused: SELECT * is not in the allowed pattern list`; `... JOIN ...` - `refused: JOIN is not in the allowed pattern list`

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

Refused: `... LIMIT ? OFFSET ?` - `refused: OFFSET ...`; a write query in a GET - `refused: write query AddInvoice in a GET action`

### Q6

**claim update** - one statement that checks and writes together:

```sql
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(held_by), held_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (held_by = 0 OR held_at <= sqlc.arg(now) - 600);
```

- `SET <col> = <value>, ...`; `WHERE <cond> [AND <cond>]...`, where `<cond>` is
  `<col> = <> < <= > >= <value>`, or one parenthesised `(<cond> OR <cond> ...)` group;
- at least one `<col> = <parameter>` outside the group (which rows);
- a parameter may be plus or minus a whole number (`sqlc.arg(now) - 600`);
- `:execrows` only, no `RETURNING`; the action checks the count (S10).

English: ``2. Claim: in table `seats`, set `held_by` = the request's `person_id` and `held_at` = the current time on each seat whose `id` is the request's `seat_id` and (`held_by` is 0 or `held_at` is 10 minutes or more before the current time) at that moment (query `ClaimSeat` in queries/claim_seat.sql). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same seat.``

**Comparisons with the current time.** Next to the current time (T1) a
comparison is said as a distance, and every phrase names its boundary: a
time exactly `d` away is inside "`d` or more", "no earlier than" and "no
later than", outside "more than `d`", "later than" and "earlier than".
With no offset the same words compare with the current time itself: an
`expires_at` equal to the current time is "no later than" and "no earlier
than" it, and neither "earlier than" nor "later than" it.
`TestClockComparisonWording`, `TestClockComparisonTable` and
`TestClockComparisonWithoutOffset` pin these lines;
the claim checks prove the `<=` boundary against SQLite (a hold taken 599
seconds earlier blocks the seat, one taken exactly 600 seconds earlier does
not).

| SQL | English |
|---|---|
| `held_at <= sqlc.arg(now) - 600` | `` `held_at` is 10 minutes or more before the current time `` |
| `held_at < sqlc.arg(now) - 600` | `` `held_at` is more than 10 minutes before the current time `` |
| `held_at > sqlc.arg(now) - 600` | `` `held_at` is later than 10 minutes before the current time `` |
| `held_at >= sqlc.arg(now) - 600` | `` `held_at` is no earlier than 10 minutes before the current time `` |
| `held_at = sqlc.arg(now) - 600` / `<>` | `` `held_at` is exactly 10 minutes before the current time `` / `is not exactly` |
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
- `WHERE held_by = 0 OR held_at <= ...` (no parentheses) - `refused: OR in WHERE is not in the allowed pattern list (query ClaimSeat)`
- `SET held_at = ? WHERE id = ? AND held_at <= ?` - `refused: second bare ? for column held_at ... name this one with sqlc.arg(<name>)`

---

## Rules across statements: T1, T2, W1

### T1

**time is passed in** - logic never reads the clock. `action.go` and
`internal/domain` never import `time`. The current time is an Input field:

```go
type Input struct {
	SeatID   int64 `json:"seat_id"`
	PersonID int64 `json:"person_id"`
	Now      int64 `json:"now" clock:"now"`
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
and in steps: `` `held_at` is 10 minutes or more before the current time `` (see Q6 for every operator).

Refused:
- `import "time"` - `refused: import "time" is not in the allowed pattern list (T1 time is passed in). Logic never reads the clock. ...`
- `now := time.Now().Unix()` - `refused: clock read time.Now().Unix is not in the allowed pattern list (T1 time is passed in)`
- `Now string \`json:"now" clock:"now"\`` - `refused: clock field Now that is not int64 tagged clock:"now" ...`

### T2

**session is passed in** - the caller's identity is a server-set input, like
the time. At most one Input field, `int64` or `string`, tagged
`server:"session"`:

```go
type Input struct {
	SeatID  int64 `json:"seat_id"`
	Session int64 `json:"session" server:"session"`
}
```

`httpx.Bind` (`runtime/httpx`) sets it from the cookie
`httpx.SessionCookie`, which is always `bridge_session`:

- exactly one `bridge_session` cookie whose value is a whole number from 1
  up, in digits only (`int64`), or 1 to 128 letters, digits, `-`, `_` or `.`
  (`string`): the field is that value;
- no such cookie, more than one, or any other value: the field is 0 (the
  empty text), and the action runs, so its own guard raises its failure
  (`if in.Session <= 0 { return Output{}, F1 }`);
- a request whose body or query string sends `session`, in any letter case,
  is answered with HTTP 400 `bad_request` and the action does not run.

Bind only reads the cookie: it never mints, signs or renews it and checks it
against no store; whoever presents the value is that session. The app issues
it (for example when it serves its web page) with an unguessable value,
`HttpOnly`, `SameSite=Lax` and, over HTTPS, `Secure`. Checks call `Handle`
with any session they like.

English (`adapter/testdata/good/release_example/release_example.en`), apart
from the fields the caller sends:
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
- `Session int64 \`json:"session" server:"user"\`` - `refused: server field Session tagged server:"user" is not in the allowed pattern list (T2 session is passed in)`
- `Session bool \`json:"session" server:"session"\`` - `refused: server field Session of type bool ...`
- `Session int64 \`json:"session" query:"session" server:"session"\`` - `refused: server field Session with a path or query tag ...`
- a `server` tag on Output - `refused: server tag on SeatID outside Input ...`
- a second `server:"session"` field - `refused: second session field Other ...`

### W1

**no check-then-write** - a Q3 or Q6 write to a table that an earlier Q1, Q2
or Q5 query of the same action read is refused. Reading and then writing lets
another call change the row in between; put the condition into the write
(Q6) and check the count (S10). Reading **after** a claim, to explain why it
changed nothing, is allowed.

Refused (`adapter/testdata/bad/check_then_write`):
```go
holds, err := a.q.SeatHolds(ctx, in.SeatID, in.PersonID) // SELECT COUNT(*) FROM seats ...
...
if holds != 0 {
	return Output{}, F1
}
claimed, err := a.q.ClaimSeat(ctx, ...)                   // UPDATE seats ...
```
`refused: write to table seats after reading it in step 2 (check-then-write) is not in the allowed pattern list (W1 no check-then-write). Do not read a row and then write it: another call can change it in between. Put the condition into the write itself ...`

Allowed (claim first, explain after):
```go
if claimed == 0 && holder.HeldBy == in.PersonID {   // holder read after the claim
	return Output{}, F2
}
```
English: ``If no seat was changed in step 2 and the found seat's `held_by` equals the request's `person_id`, stop with F2 ...``

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
`SessionRule`, `SessionValue`, `ServerSetWhen` and `ErrorBody`;
`bridge-en` quotes the values compiled into it, which are the app's because
`go.mod` pins the same version (the pin check above), and httpx's own tests
prove each one. `cmd/server` binds every route with the runtime's
`httpx.Bind` over queries built on its `txn.DB`: one transaction per call
(`BEGIN IMMEDIATE` at the first query for writes, read-only for GET).

English: `Steps 3 to 7 run in one database transaction. It begins with the query in step 3 and holds the database's write lock until it ends, ...`

Refused: a route bound over a plain `*sql.DB`, or with an app's own `httpx` - `cmd/server: create_invoice.Route is not served with github.com/pierre10101/go-ai-bridge/runtime/httpx.Bind over queries built on github.com/pierre10101/go-ai-bridge/runtime/txn.DB; ...`

---

## Conditional claim and state-transition rules

For any action that moves a row from one state to another only if a
condition holds at that moment: claiming a resource, confirming or releasing
a hold, approving a request, consuming a one-time token. The seat hold in
`adapter/testdata/good/claim_example` is only an example.

1. **Pass the current time and the session in** (T1, T2). No clock inside
   logic, and the caller never names who they are in the body. A time
   condition is written next to the server-set `now`, and the English names
   its boundary exactly (Q6 table): `held_at <= sqlc.arg(now) - 600` is
   "`held_at` is 10 minutes or more before the current time", so a hold taken
   exactly 600 seconds ago has expired and one taken 599 seconds ago has not.
   Checks inject the time and test both sides of the boundary.
2. **Check and write in one statement** (Q6 + S10 + W1). One `UPDATE`
   changes the row only if it is in the expected state, and the action stops
   unless exactly one row changed. The English says the condition holds "at
   that moment" and "is checked by the same statement that writes".
   Check-then-write is refused.
3. **Write the failure cases before the code**, in `intent.md`, each in plain
   English with its exact boundary. For a transition, consider at least:
   - **not in the expected state** - for example the resource is held by
     someone else and the hold was taken less than the hold time before now;
   - **expired** - the hold, token or offer was taken the hold time or more
     before now;
   - **already done** - the same transition was applied before (confirmed
     twice, released twice);
   - **someone else's** - the row belongs to another person;
   - **no such row**.

   Each becomes an F-ID raised by a guard after the claim (`claimed == 0 && ...`
   reads that explain why nothing changed, allowed by W1), with the final
   `claimed != 1` guard as the catch-all (S10). The English of a
   `claimed == 0 && ...` guard says nothing was written (S10).

## Hard limits

- `Handle`: at most 70 lines. `action.go`: at most 300 lines. Input: at most 10 fields.
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
