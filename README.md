# bridge-en

**bridge-en turns Go feature code into plain English that a person can review
in minutes.** You (or an AI agent) write each feature as a small Go file plus
SQL queries, in a deliberately narrow pattern list. `bridge-en` translates it
into a fixed-template English description: the request it accepts, every step
in order, every failure with its HTTP status, what is written and what is
rolled back. The English is committed next to the code (`<slice>.en`), so a
pull request shows what the code *does* in words, not just in Go.

It is deterministic: no AI, no heuristics, the same code always gives the same
English. Anything outside the pattern list is **refused** with `file:line:col`
and the rule to follow, and CI fails. Each feature also starts with an
`intent.md` (what was asked, with numbered failure cases F1, F2, ...), and
`bridge-en` refuses code whose failure cases differ from the intent. Reviewers
compare the two: what was asked, and what was built.

- Every action declares who may call it (`httpx.Roles(...)` or
  `httpx.Public`); the runtime refuses anyone else with 401 or 403 before the
  action runs. Sign-in, passwords and sessions stay in your app; it tells the
  runtime who is signed in through one hook (`httpx.Identify`).
- A table whose rows belong to a user says so in `schema.sql`
  (`-- owner: organizer_id`); `-check` then refuses any write to it that is
  not limited to the signed-in user's rows, unless only roles the app marks
  as bypassing ownership (`httpx.AppRoles(...).BypassOwnership("admin")`)
  may call the action. The English says "only events you own".
- A table whose rows belong to rows of an owned table names its parent
  (`-- owner: event_id -> events.organizer_id`, chains allowed); every write
  to it must prove, in the statement that writes, that the parent row is the
  signed-in user's (an insert from the caller's parent row, or an
  `IN (SELECT ...)` proof subquery). The English says "only sections of
  events you own".
- A delete (Q10) names its rows by the table's key and checks the count
  (`if deleted != 1`); on owned and child tables it is limited to the
  caller's rows exactly like an update. Every foreign key that points at
  the table must say ON DELETE CASCADE or ON DELETE RESTRICT, and the
  English says which: "Each section ... is deleted with it" or "... schema.sql
  refuses the delete (ON DELETE RESTRICT)".
- A keyset list (Q5) may be the whole table (`WHERE id < ? ORDER BY id DESC
  LIMIT ?`); `path:"id"` works on PATCH/POST/DELETE as well as GET (filled
  from the URL, never from the body); `after=0` means the start of a list.
- Requests are strict: a body field or a GET query parameter the action
  does not declare is answered with HTTP 400.
- [RULEBOOK.md](RULEBOOK.md) is the reference: every rule with an example, its
  English and a refused example. `bridge-en -grammar` prints the rule list.
- One Go module, `github.com/pierre10101/go-ai-bridge`: the `bridge-en`
  binary and the small runtime (`.../runtime/...`) the app imports. An app
  never copies bridge-en source.

**Contents:** [Quickstart](#quickstart-5-minutes) ·
[Reviewing a pull request](#reviewing-a-pull-request) ·
[CI](#set-up-ci) · [Upgrading](#upgrading) · [CLI](#cli) · [FAQ](#faq) ·
[This repository](#this-repository)

## Quickstart (5 minutes)

You need Go 1.24 and [sqlc](https://sqlc.dev).

**1. Install, pinned.** The app's `go.mod` is the pin; the binary must be the
same version (`-check` refuses any other).

```sh
mkdir seat-app && cd seat-app && git init -q
go mod init example.com/seat-app
go get github.com/pierre10101/go-ai-bridge@v0.7.0
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.7.0
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
bridge-en -version                      # bridge-en 0.7.0
```

**2. Init.** Writes the instructions for AI agents (and you). Documents only,
never code; existing files are kept unless `-force`.

```sh
bridge-en init
# wrote     AGENTS.md                          the workflow (single source of truth)
# wrote     .cursor/rules/bridge-en.mdc        pointers: "follow AGENTS.md" + the 5 key rules
# wrote     CLAUDE.md
# wrote     .github/copilot-instructions.md
# wrote     GEMINI.md
```

**3. The app's skeleton.** `schema.sql` (tables), `schema.go` (embeds it for
`store.Open`), `sqlc.yaml` (one entry per feature), `cmd/server/main.go` and
`cmd/server/routes.go`. See [RULEBOOK.md, App layout](RULEBOOK.md#app-layout).

**4. The first feature, intent first.** `features/hold_seat/intent.md`:

```markdown
# Intent: Hold seat

## Why
A visitor holds a seat for 10 minutes before buying it. The visitor is the
session from the cookie, never an id sent in the request.

## Failure cases
- F1: the seat is held and its hold expires later than now (a hold whose
  `expires_at` is exactly now has expired), or the seat does not exist:
  nothing changed.
- F2: there is no valid session cookie (the session is empty): nothing
  is written.
```

The action declares who may call it, ``var Roles = httpx.Public`` (anyone,
signed in or not; an action for some users only says
``httpx.Roles("organizer", "admin")``), and takes the caller from the server,
never from the request body: ``Session string `json:"session" server:"session"` ``
(from the session cookie) and ``Now int64 `json:"now" clock:"now"` `` (the
server's clock); a signed-in user would be ``User int64 `json:"user" server:"user"` ``.
The claim stores when the hold ends and compares it with `now`:

```sql
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600
WHERE id = sqlc.arg(id) AND (held_by = '' OR expires_at <= sqlc.arg(now));
```

Then `sqlc generate`, `action.go`, one check per F-ID in `checks/`, and one
line in `routes.go`: `mux.Handle(hold_seat.Route, httpx.Bind(hold_seat.Roles, ...))`. AGENTS.md has this feature in full; an agent can write
it from there.

**5. Check, then write the English.**

```sh
bridge-en -check features/hold_seat/    # after every edit
```

Refusals tell you exactly what to change:

```
features/hold_seat/intent.md:7:1: refused: line "- F1 the seat is held ..." in "## Failure cases" is not in the allowed pattern list (I2 failure cases). Under the heading "## Failure cases", write one line per failure case, "- F<n>: <text>" ...
features/hold_seat/action.go:31:2: refused: for loop is not in the allowed pattern list (Handle body). Allowed here: S1 precondition, S2 guard, ...
```

When `-check` only says the `.en` is missing or stale:

```sh
bridge-en -write features/hold_seat/    # writes features/hold_seat/hold_seat.en
```

**6. Read the `.en` against the intent.** Every F-ID must be there with the
same boundary. An excerpt:

```
1. If the session from the cookie equals the text "", stop with F2: HTTP 401 Unauthorized "session is required".
2. Claim: in table `seats`, set `held_by` = the session from the cookie and `expires_at` = 10 minutes after the current time on each seat whose `id` is the request's `seat_id` and (`held_by` is the text "" or `expires_at` is no later than the current time) at that moment ...
3. If not exactly one seat was changed in step 2, stop with F1: HTTP 409 Conflict "seat is already held".
```

If the English says something the intent did not ask for, the code is wrong
(or the intent is). Never edit the `.en` by hand. Commit it with the code.

## Reviewing a pull request

The `pr-english` action comments on every pull request, once, updated on each
push. For each changed feature it shows:

- **Changed:** the files of that feature in the pull request;
- **Intent:** `intent.md` in full, or its diff when the pull request changes it;
- **English:** the diff of `<slice>.en`, or "unchanged" when only the code's
  form changed.

Review in this order:

1. Read the intent. Is this what was asked? Is every failure case there, with
   an exact boundary ("10 minutes or more", "exactly now")?
2. Read the English diff against it. Every `F<n>` of the intent appears in the
   English with the same meaning; nothing extra is written, read or allowed.
3. Only then skim the Go. CI already proved it matches the English, the
   intent's F-IDs match the code, and each F-ID has a check against SQLite.

Example (trimmed):

````markdown
### `features/hold_seat`

Changed: `hold_seat.en`, `intent.md`, `queries/claim_seat.sql`

**Intent** (`intent.md`, changed in this pull request (diff)):

```diff
@@ -1,4 +1,4 @@
 # Intent: Hold seat
 
 ## Why
-A visitor holds a seat for 10 minutes before buying it. The visitor is the
+A visitor holds a seat for 15 minutes before buying it. The visitor is the
```

**English** (`hold_seat.en`, diff):

```diff
-2. Claim: in table `seats`, set `held_by` = the session from the cookie and `expires_at` = 10 minutes after the current time on each seat ...
+2. Claim: in table `seats`, set `held_by` = the session from the cookie and `expires_at` = 15 minutes after the current time on each seat ...
```
````

## Set up CI

```yaml
name: CI
on: [pull_request, push]
permissions:
  contents: read
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.24.x" }
      - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.7.0   # the version go.mod pins
      - run: go test ./...
      - run: bridge-en -check features/*/
  english:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write          # the only write: the one PR comment
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }    # the comment diffs against the PR's base
      - uses: actions/setup-go@v5
        with: { go-version: "1.24.x" }
      - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.7.0
      - uses: pierre10101/go-ai-bridge/.github/actions/pr-english@v0.7.0
        # with: { features: "features/*/", max-chars: "60000" }
```

- `setup-bridge-en` installs the binary of the version `go.mod` requires
  (`with: { method: release }` downloads the released binary and checks its
  SHA256 instead of `go install`).
- `pr-english` runs `bridge-en pr-comment` and creates or updates one comment
  marked `<!-- bridge-en:pr-english -->`, at most 60000 characters. It also
  writes the text to the job summary. On pull requests from forks (read-only
  token) it only writes the summary.
- Protect `main`: changes arrive through pull requests only.

## Upgrading

```sh
go get github.com/pierre10101/go-ai-bridge@v<new>
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v<new>
bridge-en -check features/*/     # fix any new refusal
bridge-en -write features/*/     # regenerate the English
bridge-en init -force            # refresh AGENTS.md and the pointer files
git diff                         # review every .en change, then open a PR
```

A new version can change the English of every feature (new wording, new
rules); the `.en` diff in that pull request shows exactly how.

### 0.6.x to 0.7.0: lists, path inputs, after=0

0.7.0 is **mostly additive**. Apps that already compile against 0.6 stay
accepted; their `.en` files may change in two places and should be
regenerated (`bridge-en -write features/*/`) then reviewed:

1. **Q5 keyset pages may omit equality filters.** A public catalog page is
   now `WHERE id < ? ORDER BY id DESC LIMIT ?` (cursor only). The dummy
   always-true flag column workaround is no longer needed.
2. **`path:"..."` works on every method**, not only GET. Bind fills the
   field from the URL; a body that also carries it is HTTP 400. The English
   lists path values apart from the JSON body (see the fixture
   `rename_event`: `PATCH /events/{id}/title`).
3. **`after=0` means the start of the list**, same as omitting `after`
   (both become `page.StartCursor`). Refuse only a negative cursor in the
   action if you still want an F-ID for that.
4. **Non-ASCII in `schema.sql` / `queries/*.sql` is refused** at `-check`
   (sqlc's SQLite parser can mis-tokenise an em dash in a comment).
5. **Optional `// bridge-en-plural:`** on a domain type overrides the naive
   last-word plural for list English.

Move the pin and the binary to v0.7.0, run `bridge-en init -force`, then
`-write` every slice and review the `.en` diff.

### 0.5.x to 0.6.0: deletes (Q10)

0.6.0 adds Q10, the first allowed DELETE. It is **not a breaking change**
for an app: DELETE was refused before, so no 0.5.x app has one; no rule
refuses anything it accepted, and its `.en` files do not change (only the
fix-it hints of some refusals now mention DELETE). To upgrade, move the pin
and the binary to v0.6.0 and run `bridge-en init -force` to get the 0.6.0
`AGENTS.md` (Q10). To add a delete:

1. Make sure every foreign key that references the table (and every table
   an ON DELETE CASCADE reaches from it) says what happens to the
   referencing rows: `REFERENCES sections (id) ON DELETE CASCADE` (they
   are deleted with it) or `ON DELETE RESTRICT` (the delete fails while
   one exists: HTTP 500, nothing deleted). No `ON DELETE`, `NO ACTION`,
   `SET NULL` and `SET DEFAULT` are refused at `-check`. Changing the
   schema of an existing database needs a migration of your own (SQLite
   rebuilds the table to change a foreign key).
2. Write the query: `:execrows`, `DELETE FROM <table> WHERE <key> =
   sqlc.arg(<key>) [AND <col> = <value>]...` (or `<key> IN
   (sqlc.slice(<name>))`), no `RETURNING`, no OR. On an owned table add
   `AND <owner col> = sqlc.arg(<owner col>)`; on a child table add the Q9
   proof `AND <table>.<fk> IN (SELECT <parent>.<key> FROM <parent> WHERE
   <parent>.<owner col> = sqlc.arg(<owner col>))`; pass `in.User` for it.
   An action only bypass roles may call needs neither.
3. Check the count: `if deleted != 1 { return Output{}, F<n> }` with a
   failure case such as "no such section of yours" (HTTP 404) in
   `intent.md`.
4. Add checks: over HTTP against SQLite, user B deleting A's row gets HTTP
   404 and nothing is deleted (see `adapter/testdata/good/delete_section`).

### 0.4.x to 0.5.0: breaking change (inherited ownership)

0.5.0 adds A5 (inherited ownership) and two SQL shapes, Q8 (insert from a
parent row) and Q9 (proof subquery). It is a **breaking change** in two
ways: (1) a table that declares `-- owner: <fk> -> <parent>.<pcol>` gets
every write refused that does not prove its parent is the caller's (opt-in
per table, as A4 was); (2) a query that uses Q8 or Q9 names every column
with its table (`sections.id`), because sqlc reports an unqualified column
as ambiguous once two tables are read. The `.en` of an app changes only
where it adopts A5 (and the S10 English of a Q8 says "added").

1. **A5 (opt-in per child table):** for each table whose rows belong to
   rows of an owned table (the sections of an event, the line items of an
   order), add, right above its `CREATE TABLE` in `schema.sql`,
   `-- owner: <fk> -> <parent>.<pcol>`: `<fk>` holds the parent's
   single-column `PRIMARY KEY` (same type), `<pcol>` is the column the
   parent's own `-- owner:` names (its owner column, or its `<fk>` if the
   parent is a child too). Do not copy the owner column onto the child to
   "scope" it: a copy proves nothing and is refused as a proof.
2. Run `bridge-en -check features/*/`. For each A5 refusal from an action a
   role without the bypass may call:
   - an INSERT becomes a Q8 insert from the parent row, `:execrows`, no
     `RETURNING`:
     `INSERT INTO sections (event_id, name) SELECT events.id, sqlc.arg(name) FROM events WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id);`
     then `if added != 1 { return Output{}, F<n> }` (S10) with a new
     failure case such as "the event does not exist, or the signed-in user
     does not own it" (HTTP 404) in `intent.md`; the action no longer gets
     the new row's id back (read it in a later step if it must answer it);
   - an UPDATE gains the Q9 proof as an AND condition:
     `AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id))`,
     with every column qualified, and never sets `<fk>`;
   - run `sqlc generate` and pass `OrganizerID: in.User`.
   If the action is really for admins only, give it `Roles` that bypass
   ownership instead; a `Public` action cannot write a child table.
3. Run `bridge-en -write features/*/` and review every `.en` diff: each
   step on a child table gains "Ownership: only sections of events you own
   (`events.organizer_id` is the signed-in user) ..." (or, for a read,
   whether it is limited to your rows).
4. Add checks: over HTTP against SQLite, user B cannot add to or change
   rows under user A's parent (HTTP 404, nothing written).
5. Run `bridge-en init -force` to get the 0.5.0 `AGENTS.md` (A5, Q8, Q9).

### 0.3.x to 0.4.0: breaking change (ownership, strict GET queries)

0.4.0 adds two rules, and it is a **breaking change**: an app whose CI
passed on 0.3.x keeps compiling, but the `.en` of every GET changes (so
`-check` fails until `-write`), `-check` refuses writes to owned tables once
you declare an owner, and a GET client that sends an undeclared query
parameter now gets HTTP 400.

1. **T4 (runtime, no code change):** a GET request whose query string has a
   parameter the action does not declare with `query:"<name>"`, also in
   another letter case (`?Limit=5`) or a path value's name, is answered with
   HTTP 400 `bad_request` and the action does not run; before, it was
   ignored. Find clients that send extra parameters (cache-busters like
   `?_=123`, tracking parameters, typos such as `?limt=5`) and stop sending
   them, or declare the value on the action.
2. **A4 (opt-in per table):** for each table whose rows belong to a user,
   add `-- owner: <col>` on the comment line right above its `CREATE TABLE`
   in `schema.sql`, where `<col>` is the column holding the signed-in user
   (`INTEGER` for ``User int64 `json:"user" server:"user"` ``, `TEXT` for a
   string user). A table without the annotation is not checked, so 0.4.0
   refuses nothing new until you add one; add it to every table that holds
   users' private rows.
3. **Admins:** if some roles may change anyone's rows, mark them once in
   `cmd/server`, chained on the role list:
   `var AppRoles = httpx.AppRoles("customer", "organizer", "admin").BypassOwnership("admin")`.
   Only an action whose `Roles` lists only such roles may write an owned
   table without the owner filter.
4. Run `bridge-en -check features/*/`. For each A4 refusal, limit the
   write to the caller's rows: add `AND <col> = sqlc.arg(<col>)` to the
   UPDATE's `WHERE` (or set `<col>` in the INSERT), run `sqlc generate`, and
   pass `<Col>: in.User` (the action takes the signed-in user from
   ``User int64 `json:"user" server:"user"` ``, never from a request field).
   A `Public` action cannot write an owned table: give it `Roles`. If the
   action is really for admins only, change its `Roles` to roles that bypass
   ownership instead. An UPDATE that changed nothing now also means "not
   yours": say so in the failure case (`intent.md`, for example "the event
   does not exist, or the signed-in user does not own it").
5. Run `bridge-en -write features/*/` and review every `.en` diff: each GET
   gains the line "The query string is as strict as a body: ..." and the
   HTTP 400 answer says "or the query string has a parameter not listed
   above"; each step on an owned table gains an "Ownership: ..." sentence
   ("only events you own (`organizer_id` is the signed-in user) can be
   changed by this step", or, for a read, whether it is limited to the
   caller's rows).
6. Add checks: over HTTP against SQLite, user B cannot change user A's row
   (nothing written), and an undeclared query parameter gets HTTP 400.
7. Run `bridge-en init -force` to get the 0.4.0 `AGENTS.md` (owned tables,
   strict GET queries).

### 0.2.x to 0.3.0: breaking change (who may call each action)

0.3.0 adds sign-in and roles, and it is a **breaking change**: every action
must now say who may call it, and `cmd/server` must pass that to
`httpx.Bind`. An app whose CI passed on 0.2.x fails `-check` (and does not
compile: `httpx.Bind` takes the Roles first) until it is migrated.
Password hashing, sign-in and sessions stay in the app; bridge-en only
receives who is signed in and their role.

1. **A1:** in every `features/<slice>/action.go`, import
   `github.com/pierre10101/go-ai-bridge/runtime/httpx` and declare, once,
   `var Roles = httpx.Public` (anyone, signed in or not: what every 0.2.x
   action was) or `var Roles = httpx.Roles("<role>", ...)` (signed-in users
   with one of these roles). There is no default; `-check` refuses an
   action without it (`action.go without a Roles declaration ... (A1 who may call it)`).
2. **A2:** if any action lists roles, declare the app's roles once in
   `cmd/server`: `var AppRoles = httpx.AppRoles("customer", "organizer", "admin")`
   (lowercase identifiers). A role an action lists must be one of them.
3. **A3:** in `cmd/server/routes.go`, bind each route with its own Roles,
   `mux.Handle(<slice>.Route, httpx.Bind(<slice>.Roles, <slice>.New(db.New(txn.DB(conn))).Handle))`,
   and serve the mux through the app's sign-in hook:
   `return httpx.Identify(AppRoles, identity, mux)`, where `identity` is a
   `func(r *http.Request) (user, role string, ok bool)` the app writes
   (typically: read its own session cookie, look the session up in its
   store). Without `httpx.Identify` nobody is signed in, so only `Public`
   routes answer.
4. **T3:** an action that needs the signed-in user takes
   ``User int64 `json:"user" server:"user"` `` (or `string`) and, if it needs
   the role, ``Role string `json:"role" server:"role"` ``. A request that
   sends `user` or `role` gets HTTP 400. A request field named `user`,
   `role`, `user_id` or `role_id` is refused: rename a field about someone
   else (for example `member_id`).
5. Run `bridge-en -check features/*/`, then `bridge-en -write features/*/`.
   Every `.en` gains one line under the route ("Who may call it: ..."), and
   role-restricted actions gain the HTTP 401 and 403 answers. Review them.
6. Update the `checks/` that build a route by hand: `httpx.Bind(<slice>.Roles, ...)`,
   behind `httpx.Identify` with a test hook for role-restricted routes. Add
   checks for 401, 403 and an allowed role that confirm nothing is written
   on 401/403.
7. Run `bridge-en init -force` to get the 0.3.0 `AGENTS.md` (the Roles
   declaration is required; the identity comes from `server:"user"`, never
   the body).

### 0.1.x to 0.2.0: breaking change (intent.md)

0.2.0 is a **breaking change**. `bridge-en -check` and `-write` now read
`intent.md` before `action.go` and refuse a slice whose `intent.md` is
missing or not in the strict format, so an app whose CI passed on 0.1.4 fails
until every feature is migrated. Migrate each `features/<slice>/`:

1. **I1:** the slice has an `intent.md`. Write one if it is missing (why,
   inputs, outputs, failure cases).
2. **I2:** exactly one heading `## Failure cases`, spelled exactly so. Under
   it, one line per failure case, `- F<n>: <text>` (a dash, a space, the
   F-ID, a colon, a space, the text); a long case continues on the next line,
   indented by two spaces. A slice with no failure case has the single line
   `None.` F-IDs appear in increasing order, each once (no F0). No other
   list item in the file may start with an F-ID. The 0.1.4 style
   `- **F1** — the customer does not exist.` becomes
   `- F1: the customer does not exist.`
3. **I3:** the F-IDs in `intent.md` are exactly the ones `action.go`
   declares. `-check` names each missing or extra ID; fix whichever side is
   wrong.
4. Run `bridge-en -check features/*/` until it is clean, then
   `bridge-en -write features/*/` and review every `.en` diff.
5. Run `bridge-en init -force` to get the 0.2.0 `AGENTS.md`. Its example
   feature now takes the caller from
   ``Session string `json:"session" server:"session"` `` and stores the
   hold's end in `expires_at`. If a feature took who the caller is from a
   request field (for example a `person_id` input), move it to the
   server-set session: a request can claim to be anyone.

## CLI

```sh
bridge-en features/hold_seat                   # print the English
bridge-en -check features/*/                   # CI: pin, intent, refusals, golden .en, checks coverage
bridge-en -write features/hold_seat            # save the golden .en (after the intent checks)
bridge-en -grammar                             # the rule list
bridge-en -version
bridge-en init [-force] [dir]                  # write AGENTS.md + agent pointer files (docs only)
bridge-en pr-comment -base <sha> features/*/   # print the PR comment markdown
```

## FAQ

**Why refuse instead of doing its best?** Because the English is only worth
reviewing if it is exactly what the code does. A translator that guesses
would give confident English for code it does not understand. A refusal
costs a rewrite in a known pattern; a wrong sentence costs a production bug
that the review approved. Each refusal names the rule and what is allowed,
so it is an instruction an AI agent can follow.

**Why sqlc?** SQL stays plain SQL that bridge-en can read in a handful of
strict shapes (count, one row, insert, keyset page, conditional claim), and
sqlc generates typed Go for it. No ORM call hides a query, so every read and
write appears in the English with its table and condition.

**What if I need a pattern that is not supported?** First try to express it
inside the rules: pure logic goes to `internal/domain` (rendered from its
body), several checks become several guards, read-then-write becomes one
conditional UPDATE. If it truly cannot be expressed, there is no waiver:
open an issue. A new pattern is a new bridge-en release with its own English
template, rulebook section and tests, and apps opt in by upgrading.

**Does the English replace tests?** No. The `checks/` of each feature run it
against real SQLite and `-check` refuses an F-ID without a check. The English
says what the code does; the checks prove it does it.

**Which AI agents does this work with?** Any. `AGENTS.md` is the single
source of truth; `bridge-en init` adds pointer files for Cursor, Claude Code,
GitHub Copilot and Gemini CLI.

## This repository

```
cmd/bridge-en/            the CLI (render, -check, -write, init, pr-comment)
adapter/                  the compiler: grammar.go (rule list), templates.go (every English word),
                          intent.go (I1-I3), parse/handle/expr.go, sql.go, domain.go, plumbing.go,
                          schema.go (keys, A4/A5 owner annotations), owner.go (A4), inherit.go (A5),
                          delete.go (Q10 keys and ON DELETE), check.go, pin.go
adapter/testdata/         a parse-only fixture app: good/ (render, with golden .en and checks),
                          bad/ (refused, want.err), bad_intent/ (intent refusals, want.err)
internal/initdocs/        the AGENTS.md and pointer templates (go:embed) for bridge-en init
internal/prcomment/       the pull request comment of bridge-en pr-comment
runtime/                  what apps import: assert, failure, httpx, page, shape, store, txn
.github/actions/          setup-bridge-en (install), pr-english (the PR comment)
.github/workflows/        ci.yml (PRs, main; comments the fixtures' intent and English), release.yml
scripts/ci.sh             everything CI runs
scripts/smoke-app.sh      a brand-new app: init (docs only), requires the module, runs the fixture checks
```

Run everything: `./scripts/ci.sh`, then `./scripts/smoke-app.sh` and
`./scripts/smoke-app.sh --go-install` (needs sqlc).

**Release:** bump `VERSION` and the `require` in `adapter/testdata/go.mod` in
a pull request; after merge, `git tag v$(cat VERSION) && git push origin
v$(cat VERSION)`; `release.yml` checks the tag, runs CI and the smoke app,
and publishes the archives, `RULEBOOK.md` and `SHA256SUMS`.

**What the tests prove** (highlights; each test's comment says more):

| Test | Proves |
|---|---|
| `adapter TestGoldenEnglish`, `TestRenderIsDeterministic` | each fixture renders to its committed `.en`, byte for byte, every time |
| `adapter TestRefusesDeliberateRuleBreaks` | each `testdata/bad/*` is refused with exactly its `want.err` |
| `adapter TestIntentRefusals`, `TestIntentFormat`, `TestGoodIntentsAreStrict` | I1-I3: no intent.md, a malformed or misplaced failure case, missing or extra F-IDs are refused with exactly `testdata/bad_intent/*/want.err`, by `-check` and `-write` alike |
| `adapter TestRulebook`, `TestCoverageNeedsReference` | intent F-IDs = action F-IDs = covered F-IDs; no dead SQL; routes bound via `httpx.Bind` over `txn.DB` |
| `adapter TestAgentsSkeletonRenders` | the example feature in the generated AGENTS.md passes I1-I3 and renders |
| `adapter TestRulebookCoversGrammar` | RULEBOOK.md has exactly one section per rule ID |
| `adapter TestRolesContract`, `TestRolesRefusals`, `TestAppRoles` | A1-A3, T3: who may call each action, the app-wide role list, the signed-in user and role in the English, and every refused form |
| `adapter TestOwnershipEnglish`, `TestOwnershipRefusals`, `TestOwnerAnnotations`, `TestBypassOwnership` | A4: the "Ownership:" sentence of every step on an owned table; an unscoped write, a write scoped to a request field or a literal, a Public writer, a given-away row, a user of another type, a malformed annotation and a malformed bypass are refused at `file:line:col` |
| `adapter TestInheritedEnglish`, `TestInheritedRefusals`, `TestInheritedAnnotations` | A5, Q8, Q9: the "Ownership:" sentence of every step on a child table (also a 3-level chain); an unproved insert or update, a proof bound to a request field or one level short, a copied owner column, a moved parent, a Public writer and every malformed inherited annotation (unknown column or table, unowned parent, wrong column, type mismatch, no key, cycle, 4-level chains) are refused at `file:line:col` |
| `adapter TestDeleteEnglish`, `TestDeleteRefusals`, `TestDeleteSchema` | Q10: the English of a delete (one row, a key list, ON DELETE CASCADE and RESTRICT, the cascaded table in the contract); a delete limited by a request field, a literal or a copied owner, without the proof, in a Public action, without its count check, and W1 on a cascaded table are refused; a delete not by the single-column key, and any foreign key (also through a cascade) without ON DELETE CASCADE or RESTRICT, are refused at `file:line:col` |
| `runtime/httpx TestStrictQueryRule` | T4: a GET with an undeclared query parameter (any letter case, a path name) is HTTP 400 and the action does not run |
| `runtime/httpx TestRolesRule`, `TestZeroAccessDeniesEveryone`, `TestPublicRule`, `TestUserAndRoleAreNeverSent` | 401 / 403 before the action runs, deny by default, the signed-in user and role filled by the server and never accepted from the request |
| `adapter TestClock*`, `TestSession*`, `TestRollbackWording`, `TestStrictClaimCheck`, `TestMultiRowClaim*`, `TestINShapes`, `TestPrimaryKeys`, `TestSQLShapes`, `TestClaimRules`, `TestRefusalsInHandle`, `TestDomainUnderGrammar`, `TestBoundNeedsTxn`, `TestCheckPin` | the exact English and refusals of each rule (see RULEBOOK.md) |
| `internal/initdocs Test*` | init writes only the five documents, keeps existing files without `-force`, and AGENTS.md covers the workflow |
| `internal/prcomment Test*` | the comment shows each changed feature's intent (full or diff) and `.en` diff, starts with its marker, is the same on every run, and stays under its size limit |
| `runtime/... Test*` | every HTTP and transaction sentence the English quotes |
| `testdata/good/*/checks` (via `smoke-app.sh`) | each fixture failure case fires against SQLite and writes nothing; boundaries; concurrency; over HTTP, 401, 403 and the allowed roles (create_event, create_invoice, list_customer_invoices), nothing written on 401/403; organizer B cannot rename organizer A's event (rename_event, nothing written) while an admin can (admin_rename_event); organizer B cannot add a section to, or rename a section of, organizer A's event (add_section, rename_section: HTTP 404, nothing written) while an admin can rename it (admin_rename_section); organizer B cannot delete organizer A's section or event (delete_section, delete_event: HTTP 404, nothing deleted), a section with seats is not deleted (ON DELETE RESTRICT) and an event's sections go with it (ON DELETE CASCADE), while an admin can delete any section (admin_delete_section); an undeclared query parameter is HTTP 400 (list_customer_invoices, my_events) |

## License

MIT. See [LICENSE](LICENSE).
