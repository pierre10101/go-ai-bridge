Follow AGENTS.md at the root of this repository: it is the workflow for this
app (bridge-en {{VERSION}}). The five rules that matter most:

1. Write `features/<slice>/intent.md` FIRST, with every failure case under
   `## Failure cases` as `- F<n>: <text>`; then queries, action.go and checks.
2. Run `bridge-en -check features/<slice>/` after every edit. A refusal is an
   instruction: fix the code to fit the rule it names (RULEBOOK.md,
   `bridge-en -grammar`); there are no waivers.
3. Never edit a `.en` file by hand: `bridge-en -write`, then read the `.en`
   against intent.md.
4. Pass the server's time, session and signed-in user in (`clock:"now"`,
   `server:"session"`, `server:"user"`, `server:"role"`); the caller is never
   an id or role sent in the request body. Every action declares who may
   call it: `var Roles = httpx.Roles("<role>", ...)` or `httpx.Public`. A
   claim is one conditional UPDATE checked by `!= 1` (or
   `!= int64(len(in.<List>))`). A write to a table declared
   `-- owner: <col>` in schema.sql is limited to `<col> = in.User` unless
   only roles marked `.BypassOwnership(...)` in cmd/server may call it; a
   child table (`-- owner: <fk> -> <parent>.<pcol>`) is written only by a
   statement that proves the parent is the user's (Q8 or Q9). A delete
   (Q10) names one row by its key, has the same owner condition or proof,
   is checked by `!= 1`, and needs ON DELETE CASCADE or RESTRICT on every
   foreign key that references its table.
5. Change code only through pull requests; never push to main. Install the
   bridge-en version that go.mod pins.
