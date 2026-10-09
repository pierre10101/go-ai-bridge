Follow AGENTS.md at the root of this repository: it is the workflow for this
app (bridge-en {{VERSION}}). The five rules that matter most:

1. Write `features/<slice>/intent.md` FIRST, with every failure case under
   `## Failure cases` as `- F<n>: <text>`; then queries, action.go and checks.
2. Run `bridge-en -check features/<slice>/` after every edit. A refusal is an
   instruction: fix the code to fit the rule it names (RULEBOOK.md,
   `bridge-en -grammar`); there are no waivers.
3. Never edit a `.en` file by hand: `bridge-en -write`, then read the `.en`
   against intent.md.
4. Pass the server's time and session in (`clock:"now"`, `server:"session"`);
   the caller is that session, never an id sent in the request body. A
   claim is one conditional UPDATE checked by `!= 1` (or
   `!= int64(len(in.<List>))`).
5. Change code only through pull requests; never push to main. Install the
   bridge-en version that go.mod pins.
