# bridge-en

`bridge-en` compiles Go feature code to controlled English that a person can
review in minutes. It is deterministic: no AI, no heuristics. It parses Go with
`go/ast` and SQL with a strict shape parser, accepts only an explicit list of
patterns, renders each with a fixed template, and refuses anything else with
`file:line:col`. If it cannot render something, CI fails.

- **[RULEBOOK.md](RULEBOOK.md)** is the prompt and the parser contract: every
  allowed pattern with a code example, the English it produces and one refused
  example.
- `bridge-en -grammar` prints the same list, one line per rule.

bridge-en is one Go module, `github.com/pierre10101/go-ai-bridge`: the
`bridge-en` binary, plus the runtime the English describes
(`github.com/pierre10101/go-ai-bridge/runtime/...`), which apps import. An app
copies no bridge-en source, and this repository ships no app.

## Install

In the app (its `go.mod` is the pin):

```sh
go get github.com/pierre10101/go-ai-bridge@v0.1.2
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v0.1.2
bridge-en -version        # bridge-en 0.1.2
```

Install the binary of the version `go.mod` requires: `bridge-en -check` and
`-write` refuse an app whose `go.mod` pins another version, because the
English quotes the runtime and must quote the one the app runs.

Or download `bridge-en_<version>_<os>_<arch>.tar.gz` and `SHA256SUMS` from the
GitHub Release `v<version>` and check the hash.

## Import the runtime

```go
import (
	"github.com/pierre10101/go-ai-bridge/runtime/assert"  // assert.Pre / assert.Post (S1, S6, M2)
	"github.com/pierre10101/go-ai-bridge/runtime/failure" // failure.New: the F-IDs (D6)
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"   // httpx.Bind: JSON in/out, one transaction per call (H1)
	"github.com/pierre10101/go-ai-bridge/runtime/page"    // page.IsPageLimit, page.NextAfter (E5, S9)
	"github.com/pierre10101/go-ai-bridge/runtime/shape"   // shape.Has in internal/domain (M3)
	"github.com/pierre10101/go-ai-bridge/runtime/store"   // store.Open(ctx, path, schema): SQLite
	"github.com/pierre10101/go-ai-bridge/runtime/txn"     // txn.DB: queries inside the call's transaction
)
```

`cmd/server/routes.go` binds each slice with one line:

```go
mux.Handle(create_invoice.Route, httpx.Bind(create_invoice.New(db.New(txn.DB(conn))).Handle))
```

Then write slices under `features/` (see
[RULEBOOK.md, App layout and Writing a slice](RULEBOOK.md#app-layout)).

## Pin it in app CI

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: "1.24.x"
- uses: pierre10101/go-ai-bridge/.github/actions/setup-bridge-en@v0.1.2
  # with:
  #   version: v0.1.2            # default: the version go.mod requires (go list -m)
  #   method: release            # download the released binary, checked with SHA256, instead of go install
  #   working-directory: .       # the app's module root
- run: bridge-en -check features/*/
```

To move an app to a new version: `go get github.com/pierre10101/go-ai-bridge@v<new>`,
install the same binary, `bridge-en -write` every slice, review the `.en`
diffs, commit.

## CLI

```sh
bridge-en features/create_invoice          # print the English
bridge-en -write features/create_invoice   # update the golden .en file
bridge-en -check features/*/               # CI: go.mod pin, refusals, golden .en, F-ID cross-checks
bridge-en -grammar                         # the allowed pattern list
bridge-en -version                         # the version
```

A refusal:

```
adapter/testdata/bad/retry_loop/action.go:47:2: refused: for loop is not in the allowed pattern list (Handle body). Allowed here: S1 precondition, S2 guard, S3 query, S4 error return, S5 let, S6 postcondition, S7 success return, S8 map each, S9 next cursor
```

## This repository

```
cmd/bridge-en/            the CLI
adapter/                  the compiler: grammar.go (the pattern list), templates.go (every English
                          word), parse/handle/expr.go (Go), sql.go (SQL shapes), domain.go,
                          plumbing.go (quotes runtime/httpx), check.go (cross-checks), pin.go (go.mod pin)
adapter/testdata/         a parse-only fixture app (module example.com/fixtures, see its go.mod):
  good/                     slices that render, each with its golden <slice>.en and checks:
                            claim_example (conditional claim), create_invoice (insert),
                            list_customer_invoices (keyset page), release_example
                            (session from the cookie; what a stop says about a claim's write)
  bad/                      deliberate rule breaks and their exact refusals (want.err)
  internal/domain/, schema.sql, cmd/server/   the fixture app's domain, schema and routes
runtime/                  the runtime apps import: assert, failure, httpx, page, shape, store, txn
version.go, VERSION       the release version
scripts/ci.sh             everything CI runs
scripts/smoke-app.sh      a new app requires the module, imports the runtime, runs the fixture checks
scripts/build-bridge-en.sh  release archives + SHA256SUMS
.github/actions/setup-bridge-en/   the install step for app CI
.github/workflows/        ci.yml (PRs, main), release.yml (tags v*.*.*)
```

Run everything: `./scripts/ci.sh`, then `./scripts/smoke-app.sh` (needs sqlc:
`go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`; add `--go-install` to
`go get` and `go install` from a throw-away local tag instead of a `replace`).

## Release

1. Bump `VERSION` in a pull request, and the `require` in
   `adapter/testdata/go.mod` with it (a grammar or template change is a new
   version: it changes the English of every app that upgrades).
2. After merge, on `main`: `git tag v$(cat VERSION) && git push origin v$(cat VERSION)`.
3. `release.yml` checks the tag equals `VERSION`, runs CI and the smoke app,
   and publishes the archives, `RULEBOOK.md` and `SHA256SUMS`. The tag is also
   what `go get …@v<version>` and `go install …@v<version>` resolve.

## What the tests prove

| Test | Proves |
|---|---|
| `adapter TestGoldenEnglish` | each `testdata/good/<slice>` renders to its committed `<slice>.en`, byte for byte |
| `adapter TestClockComparisonWording`, `TestClockComparisonTable`, `TestClockComparisonWithoutOffset`, `TestClockArgInDomainCall` | the exact English of `<=`, `<`, `>`, `>=` (and `=`, `<>`) against the current time, before, after and with no offset |
| `adapter TestSessionContract`, `TestSessionRules` | a `server:"session"` input is listed apart from the body fields with `httpx.SessionRule`; other shapes are refused (T2) |
| `adapter TestRollbackWording` | a stop says "The write in step N is rolled back" only where a write changed rows; "Any change made" while a claim's count is unknown; "Nothing was written" under `claimed == 0` |
| `adapter TestRefusesDeliberateRuleBreaks` | each `testdata/bad/*` is refused with exactly its `want.err` (retry loop, hidden magic, SQL shapes, check-then-write, unchecked claim, clock inside) |
| `adapter TestRefusalsInHandle`, `TestSQLShapes`, `TestClaimRules`, `TestDomainUnderGrammar` | single constructs outside the grammar are refused with file:line:col |
| `adapter TestRulebook` | intent.md F-IDs = action F-IDs = covered F-IDs; no dead SQL; routes bound via the runtime's httpx.Bind over txn.DB |
| `adapter TestBoundNeedsTxn` | a route over a plain `*sql.DB`, or through an app's own httpx, is refused |
| `adapter TestCheckPin`, `TestFixtureAppPinsThisVersion` | `-check` refuses a `go.mod` that pins another version, or none |
| `adapter TestRulebookCoversGrammar` | RULEBOOK.md has exactly one section per grammar rule |
| `runtime/httpx Test*` (incl. `TestClockRule*`, `TestSessionRule*`, `TestServerSet*`, `TestTx*`) | every HTTP and transaction sentence the English quotes |
| `runtime/{assert,page,shape,store} Test*` | the other runtime primitives the English relies on |
| `testdata/good/*/checks` (run by `smoke-app.sh`) | each fixture failure case fires and writes nothing; the claim's 599/600-second boundary |

## License

MIT. See [LICENSE](LICENSE).
