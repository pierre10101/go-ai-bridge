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
go get github.com/pierre10101/go-ai-bridge@v0.2.0
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.2.0
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
bridge-en -version                      # bridge-en 0.2.0
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

The action takes the caller from the server, never from the request body:
``Session string `json:"session" server:"session"` `` (from the session
cookie) and ``Now int64 `json:"now" clock:"now"` `` (the server's clock). The
claim stores when the hold ends and compares it with `now`:

```sql
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600
WHERE id = sqlc.arg(id) AND (held_by = '' OR expires_at <= sqlc.arg(now));
```

Then `sqlc generate`, `action.go`, one check per F-ID in `checks/`, and one
line in `routes.go`. AGENTS.md has this feature in full; an agent can write
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
      - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.2.0   # the version go.mod pins
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
      - uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.2.0
      - uses: pierre10101/go-ai-bridge/.github/actions/pr-english@v0.2.0
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
                          schema.go, check.go, pin.go
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
| `adapter TestClock*`, `TestSession*`, `TestRollbackWording`, `TestStrictClaimCheck`, `TestMultiRowClaim*`, `TestINShapes`, `TestPrimaryKeys`, `TestSQLShapes`, `TestClaimRules`, `TestRefusalsInHandle`, `TestDomainUnderGrammar`, `TestBoundNeedsTxn`, `TestCheckPin` | the exact English and refusals of each rule (see RULEBOOK.md) |
| `internal/initdocs Test*` | init writes only the five documents, keeps existing files without `-force`, and AGENTS.md covers the workflow |
| `internal/prcomment Test*` | the comment shows each changed feature's intent (full or diff) and `.en` diff, starts with its marker, is the same on every run, and stays under its size limit |
| `runtime/... Test*` | every HTTP and transaction sentence the English quotes |
| `testdata/good/*/checks` (via `smoke-app.sh`) | each fixture failure case fires against SQLite and writes nothing; boundaries; concurrency |

## License

MIT. See [LICENSE](LICENSE).
