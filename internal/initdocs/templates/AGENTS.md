# AGENTS.md: how to change this app

This file is for any AI coding agent (and any person) working in this
repository. It was written by `bridge-en init` (bridge-en {{VERSION}}).

The features of this app are written in a narrow Go + sqlc grammar. The tool
`bridge-en` translates each feature, deterministically and without AI, into
English (`<slice>.en`) that a person reviews. Code outside the grammar is
refused. The rules are in `RULEBOOK.md` of the pinned version
(https://github.com/pierre10101/go-ai-bridge/blob/v{{VERSION}}/RULEBOOK.md),
and `bridge-en -grammar` prints them, one line per rule ID.

## 1. Install the pinned version

`go.mod` is the pin. Use the version it requires; never a different one.

```sh
go get github.com/pierre10101/go-ai-bridge@v{{VERSION}}          # once, in a new app
go list -m github.com/pierre10101/go-ai-bridge                    # the pinned version
go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v{{VERSION}}   # the same version
bridge-en -version                                                # bridge-en {{VERSION}}
```

Import the runtime (`github.com/pierre10101/go-ai-bridge/runtime/...`). Never
copy bridge-en code into the app.

## 2. Write one feature, in this order

1. `features/<slice>/intent.md` FIRST: why, inputs, outputs and every failure
   case, before any code. bridge-en parses one section of it, exactly:

   ```
   ## Failure cases
   - F1: <one failure, in plain English, with its exact boundary>
   - F2: <a long case continues on the next line,
     indented by two spaces>
   ```
2. `features/<slice>/queries/*.sql` in the SQL shapes (Q0-Q7), then
   `sqlc generate`.
3. `features/<slice>/action.go` in the grammar (D1-D10, S1-S11, E1-E7, T1-T3,
   A1-A3). The F-IDs it declares are exactly those of intent.md. It declares
   who may call it (A1, required): `var Roles = httpx.Roles("organizer",
   "admin")` for signed-in users with one of those roles (each role one of the
   app's, declared once in `cmd/server` with `httpx.AppRoles`), or
   `var Roles = httpx.Public` for anyone, signed in or not. There is no
   default: `-check` refuses an action without it.
4. `features/<slice>/checks/*_test.go`: one `func TestF<n>_...` per F-ID that
   references `<slice>.F<n>` and runs the action against real SQLite
   (`store.Open(ctx, ":memory:", schema)`), proving the failure fires and
   writes nothing. Test both sides of every time boundary.
5. One line in `cmd/server/routes.go`, passing the slice's own Roles:
   `mux.Handle(<slice>.Route, httpx.Bind(<slice>.Roles, <slice>.New(db.New(txn.DB(conn))).Handle))`.
   `Routes` returns `httpx.Identify(AppRoles, identity, mux)`, where
   `identity` is the app's sign-in hook. Sign-in, password hashing and
   sessions are the app's own code (outside `features/`); the hook only
   tells the runtime who is signed in and with which role.

## 3. Check after every edit

```sh
bridge-en -check features/<slice>/     # after EVERY edit
bridge-en -write features/<slice>/     # save the English when -check only says the .en is stale
```

- A refusal is an instruction. It names `file:line:col`, the rule ID and what
  is allowed instead. Change the code to fit; there are no waivers. Look the
  rule up in RULEBOOK.md or `bridge-en -grammar`.
- After `-write`, read `<slice>.en` against `intent.md`: every F-ID, every
  boundary, every input. If they disagree, fix the code or the intent.
- Never edit a `.en` file by hand. Never edit `db/` (sqlc writes it).

## 4. Rules that matter most

- **Server state is passed in, never read.** The current time is an Input
  field ``Now int64 `json:"now" clock:"now"` ``; the caller's session is
  ``Session string `json:"session" server:"session"` `` (or int64); the
  signed-in user is ``User int64 `json:"user" server:"user"` `` (or string)
  and their role ``Role string `json:"role" server:"role"` ``. Who the caller
  is comes only from these, never from the request body (no `person_id`,
  `user_id` or `role` input: a request that sends `user` or `role` gets
  HTTP 400). Never call `time.Now()` or read a cookie in a feature.
- **Every action declares who may call it.** `var Roles =
  httpx.Roles("<role>", ...)` or `var Roles = httpx.Public`; the runtime
  answers 401 (not signed in) or 403 (role not listed) before the action
  runs. Never check a role inside `Handle` instead.
- **Claims are one conditional UPDATE.** Check and write in one statement
  (`UPDATE ... WHERE id = ? AND <condition>`, `:execrows`), then stop unless
  exactly one row changed: `if n != 1 { return Output{}, F<n> }`. For a list,
  `id IN (sqlc.slice(ids))` last and `if n != int64(len(in.IDs))`. Never read
  a row and then write it.
- **Keep the UI thin.** It renders only what the server returns; it holds no
  business rule and decides nothing the server did not.
- **Count down from the server's clock.** Show remaining time as
  `expires_at - now`, both from the server's answer, never from the device clock.
- **Map errors on `error.id`.** Every failure answers
  `{"error": {"id": "F2", "message": "..."}}`; the UI branches on `id`, never
  on the message or the HTTP status alone.
- **Change code only through pull requests.** Never push to main. CI runs
  `bridge-en -check` and comments each changed feature's intent next to its
  English for the reviewer.

## 5. Example feature skeleton (text only)

```
features/hold_seat/
  intent.md          the failure cases, first
  queries/claim_seat.sql
  db/                sqlc generate
  action.go
  checks/hold_seat_test.go
  hold_seat.en       bridge-en -write
```

`intent.md`:

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

`queries/claim_seat.sql`:

```sql
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600
WHERE id = sqlc.arg(id) AND (held_by = '' OR expires_at <= sqlc.arg(now));
```

`action.go`:

```go
package hold_seat

import (
	"context"
	"net/http"

	"example.com/app/features/hold_seat/db"
	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/failure"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

const Route = "POST /holds"

// Anyone may hold a seat, signed in or not: the holder is the session.
var Roles = httpx.Public

type Input struct {
	SeatID  int64  `json:"seat_id"`
	Session string `json:"session" server:"session"`
	Now     int64  `json:"now" clock:"now"`
}

type Output struct {
	SeatID int64 `json:"seat_id"`
	Now    int64 `json:"now"`
}

var (
	F1 = failure.New("F1", http.StatusConflict, "seat is already held")
	F2 = failure.New("F2", http.StatusUnauthorized, "session is required")
)

type Action struct {
	q *db.Queries
}

func New(q *db.Queries) *Action { return &Action{q: q} }

func (a *Action) Handle(ctx context.Context, in Input) (Output, error) {
	if in.Session == "" {
		return Output{}, F2
	}
	claimed, err := a.q.ClaimSeat(ctx, db.ClaimSeatParams{Session: in.Session, Now: in.Now, ID: in.SeatID})
	if err != nil {
		return Output{}, err
	}
	if claimed != 1 {
		return Output{}, F1
	}
	out := Output{SeatID: in.SeatID, Now: in.Now}
	assert.Post(out.Now == in.Now, "the answer carries the server's clock")
	return out, nil
}
```

An action only some users may call declares their roles and takes the
signed-in user from the server, for example:

```go
var Roles = httpx.Roles("organizer", "admin")

type Input struct {
	Title string `json:"title"`
	User  int64  `json:"user" server:"user"` // the signed-in user, never from the body
}
```

The schema behind it: `seats (id INTEGER PRIMARY KEY, held_by TEXT NOT NULL
DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0)`; `held_by` is the
session holding the seat ('' when free) until `expires_at` (unix seconds).

`checks/hold_seat_test.go`: `TestF1_HoldNotExpired` (a hold whose
`expires_at` is one second after `Now` blocks; one whose `expires_at` equals
`Now` has expired and does not; nothing is written while it blocks) and
`TestF2_NoSession` (an empty session changes nothing), each asserting
`errors.Is(err, hold_seat.F<n>)`.
