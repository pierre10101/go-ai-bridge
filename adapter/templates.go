package adapter

// Every English word the adapter itself emits comes from this file. The only
// other English in a .en file is quoted: `// bridge-en:` display names of
// internal/domain types, the declared outcomes and rules of runtime/httpx,
// F-ID messages and assertion reasons. Domain functions are rendered from
// their bodies with the templates below (domainTemplates). Changing a template changes every golden file, on
// purpose: reviewers see the diff.

// Expression templates (E-patterns). %s slots are filled with rendered parts.
var exprTemplates = map[string]string{
	"int":          "%s",
	"string":       "the text %s",
	"true":         "true",
	"false":        "false",
	"nil":          "nothing",
	"==":           "%s equals %s",
	"!=":           "%s does not equal %s",
	"<":            "%s is less than %s",
	"<=":           "%s is at most %s",
	">":            "%s is greater than %s",
	">=":           "%s is at least %s",
	"!= nil":       "%s is present",
	"== nil":       "%s is absent",
	"&&":           "%s and %s",
	"||":           "%s or %s",
	"not":          "it is false that %s",
	"bool name":    "%s is true",
	"paren":        "(%s)",
	"record":       "%s with %s",
	"empty record": "%s with nothing set",
	"field":        "`%s` = %s",
	// Names (E1). No Go identifier reaches the English: the request and the
	// answer are named by their JSON fields, query results by their table.
	"request":       "the request",
	"request field": "the request's `%s`",
	"clock":         "the current time",
	"session":       "the session from the cookie",
	"user":          "the signed-in user",
	"role":          "the signed-in user's role",
	"context":       "the request context",
	"action":        "the action",
	"queries":       "the database access",
	"answer":        "the answer",
	"answer field":  "the answer's `%s`",
	"row":           "the %s",
	"row field":     "the %s's `%s`",
	"new row":       "new %s",
	"found row":     "found %s",
	"listed rows":   "listed %s",
	"listed row":    "listed %s",
	"list":          "the %s built in step %d",
	"next cursor":   "the next cursor",
	"let":           "`%s`",
	"let field":     "the `%s` of `%s`",
	// Count results (Q1) in conditions and values.
	"count none":  "no %s has %s",
	"count some":  "at least one %s has %s",
	"count value": "the number of %s whose %s",
	// A count that compares with the current time (Q1, T1) keeps the Q6
	// boundary words ("is no later than"), so its guard says "whose".
	"count none whose": "there is no %s whose %s",
	"count some whose": "there is at least one %s whose %s",
	"where is":         "`%s` is %s",
	"where has":        "`%s` equal to %s",
	// Q7 IN lists in reads and claims.
	"where is in":  "`%s` is one of %s",
	"where has in": "`%s` equal to one of %s",
	"where in":     "`%s` is one of %s",
	// SQL values (Q4, Q3 next number).
	"next number": "one more than the largest `%s` in `%s` (1 if there is none)",
	// Q6 claim conditions and values.
	"where =":  "`%s` is %s",
	"where <>": "`%s` is not %s",
	"where <":  "`%s` is less than %s",
	"where <=": "`%s` is at most %s",
	"where >":  "`%s` is greater than %s",
	"where >=": "`%s` is at least %s",
	"offset +": "%s plus %s",
	"offset -": "%s minus %s",
	"time +":   "%s after %s",
	"time -":   "%s before %s",
	// A column compared with the current time plus or minus an amount (T1,
	// Q6) is said as its distance from the current time, boundary included
	// or excluded in so many words (see clockComparison).
	"clock - =":  "`%s` is exactly %s before %s",
	"clock - <>": "`%s` is not exactly %s before %s",
	"clock - <=": "`%s` is %s or more before %s",
	"clock - <":  "`%s` is more than %s before %s",
	"clock - >":  "`%s` is later than %s before %s",
	"clock - >=": "`%s` is no earlier than %s before %s",
	"clock + =":  "`%s` is exactly %s after %s",
	"clock + <>": "`%s` is not exactly %s after %s",
	"clock + >=": "`%s` is %s or more after %s",
	"clock + >":  "`%s` is more than %s after %s",
	"clock + <":  "`%s` is earlier than %s after %s",
	"clock + <=": "`%s` is no later than %s after %s",

	// The same with no offset: the value against the current time itself.
	"clock =":  "%s is exactly %s",
	"clock <>": "%s is not exactly %s",
	"clock <=": "%s is no later than %s",
	"clock <":  "%s is earlier than %s",
	"clock >":  "%s is later than %s",
	"clock >=": "%s is no earlier than %s",

	"seconds":  "%d seconds",
	"minutes":  "%d minutes",
	"hours":    "%d hours",
	"days":     "%d days",
	"1 second": "1 second",
	"1 minute": "1 minute",
	"1 hour":   "1 hour",
	"1 day":    "1 day",
	// Q6 claim results (S10) in conditions and values.
	"changed none":    "no %s was changed in step %d",
	"changed one":     "exactly one %s was changed in step %d",
	"changed not one": "not exactly one %s was changed in step %d",
	"changed value":   "the number of %s changed in step %d",
	// S11: a claim over a Q7 IN list against the length of its list.
	"changed not len": "the number of %s changed in step %d is not the number of %s in %s",
}

// Domain function templates (M2/M3), used to render a domain function's body.
var domainTemplates = map[string]string{
	"param":       "{%s}",
	"one of":      "%s is one of %s",
	"between":     "%s is between %s and %s (both included)",
	"not between": "%s is not between %s and %s (both included)",
	"not one of":  "%s is not one of %s",
	"shape":       "%s is %s",
	"digit":       "one digit",
	"digits":      "%s digits",
	"padded":      "%s as %s digits",
	"followed by": " followed by ",
}

func dtpl(key string) string { return domainTemplates[key] }

// Field type templates (D4/D5).
var typeTemplates = map[string]string{
	"int64":  "a whole number",
	"string": "text",
	"bool":   "true or false",
	"list":   "a list (possibly empty) of %s",
}

const (
	structTypeTemplate  = "%s, as an object with %s"
	structFieldTemplate = "`%s` (%s)"
)

// Step templates (S2-S7, one numbered step each, in code order).
var stepTemplates = map[string]string{
	"guard":          "If %s, stop with %s: HTTP %s %q.",
	"count":          "Read: count the %s whose %s (query `%s` in %s).",
	"row":            "Read: find a %s whose %s (query `%s` in %s); if several match, the first row returned is used. Call it the found %s.",
	"page":           "Read: list the %s whose %s and whose `%s` is less than %s, highest `%s` first, at most %s of them (query `%s` in %s). Call them the listed %s; there may be none.",
	"next cursor":    "Let the next cursor be the `%s` of the last listed %s if the page is full (there are %s listed %s), otherwise 0: there is no next page.",
	"read return":    "End the read-only transaction, then answer HTTP %s with %s. If ending it fails, stop with HTTP %s.",
	"insert":         "Write: add one %s to table `%s` with %s (query `%s` in %s). Call the stored row the new %s.",
	"claim":          "Claim: in table `%s`, set %s on each %s whose %s at that moment (query `%s` in %s). The condition is checked by the same statement that writes, never by an earlier read, so two calls cannot both change the same %s.",
	"map each":       "For each of the listed %s, build %s:",
	"map each empty": "If there are no listed %s, that list is empty.",
	"query fails":    "If the query fails, stop with HTTP %s.",
	"row fails":      "If no %s matches or the query fails, stop with HTTP %s.",
	"insert fails":   "If the query fails (for example a rule in schema.sql rejects the row), stop with HTTP %s.",
	"answer":         "Build the answer:",
	"let":            "Let `%s` be %s.",
	"let record":     "Let `%s` be %s with:",
	"post":           "Check that:",
	"post note":      "If any of these is false, it is a bug: stop with HTTP %s.",
	"domain asserts": "While doing this, internal/domain asserts that %s; if not, it is a bug: stop with HTTP %s.",
	"rolled back 1":  "The write in step %d is rolled back.",
	"rolled back n":  "The writes in steps %s are rolled back.",
	"return":         "Answer HTTP %s with %s.",
	"commit return":  "Commit the transaction, then answer HTTP %s with %s. If the commit fails, stop with HTTP %s: nothing is written.",
	"assertion":      "%s (%q)",
	"quoted":         "%q",
	"params":         "the values for query `%s`",

	// A claim (Q6) whose changed-row count is not known at this step, and a
	// claim that a guard's condition says changed nothing (see wroteMaybe,
	// wroteNone in handle.go).
	"maybe rolled back 1": "Any change made in step %d is rolled back.",
	"maybe rolled back n": "Any changes made in steps %s are rolled back.",
	"nothing written 1":   "Nothing was written in step %d, so there is nothing to roll back.",
	"nothing written n":   "Nothing was written in steps %s, so there is nothing to roll back.",
}

// Contract and index sentences.
var docSentences = map[string]string{
	"status":          "%d %s",
	"success":         "HTTP %s on success, with an object of %s:",
	"fields one":      "this field",
	"fields many":     "these %d fields",
	"field":           "`%s`: %s.",
	"outcome":         "HTTP %s, id %q, %s, if %s.",
	"message fixed":   "message %q",
	"message varies":  "with a message describing the problem",
	"failure status":  "HTTP %s, id %s with that failure case's message, if a failure case applies (see Steps).",
	"query fails at":  "a query fails (%s)",
	"assert fails at": "an assertion fails (%s)",
	"commit fails at": "the commit fails (%s)",
	"step":            "step %d",
	"steps":           "steps %s",
	"before step 1":   "before step 1",
	"reads":           "reads %s",
	"writes":          "writes %s",
	"data":            "It %s, only through the queries in queries/, inside one database transaction (see Steps).",
	"no data":         "It touches no table.",
	"no pre":          "None asserted.",
	"get input":       "The request has no JSON body. It takes these values from the path and the query string:",
	"get no input":    "The request has no JSON body and takes no value from the path or the query string.",
	"path field":      "`%s`, from the path (`{%s}`): %s.",
	"query field":     "`%s`, from the query string (may be left out): %s.",
	"page contract":   "Pages use keyset cursors, not OFFSET. A page has at most %[2]d rows; when `limit` is left out, at most %[3]d.",
	"page next":       "Pages use keyset cursors, not OFFSET: to get the next page, send the answer's `%[1]s` as `after`; `%[1]s` is 0 when there is no next page. A page has at most %d rows; when `limit` is left out, at most %d.",
	"end fails at":    "ending the read-only transaction fails (%s)",
	"read data":       "It %s, only through the queries in queries/, inside one read-only database transaction (see Steps).",
	"failure index":   "%s %q: HTTP %s; %s, %s.",
	"read index":      "%s %q: HTTP %s; %s.",
	"before write":    "before any write",
	"after write":     "after a write, which is rolled back",

	// What a claim that may have changed nothing leaves (see handle.go), and
	// the inputs the server sets (T1, T2).
	"after maybe write": "after a write that may have changed rows; any change it made is rolled back",
	"after no write":    "after a write that changed nothing, so nothing was written",
	"server intro 1":    "The action also takes this value, which the caller does not send:",
	"server intro n":    "The action also takes these %d values, which the caller does not send:",
	"server set when":   "%s; or %s",
}

// docTemplate renders one slice in a fixed layout: contract (with every HTTP
// answer), preconditions, steps in code order, failure-case index.
const docTemplate = `# {{.Title}}

Generated by bridge-en from {{.Package}}/action.go, its queries/*.sql, internal/domain and bridge-en's runtime/httpx. Do not edit; regenerate.

## Contract

The action "{{.Title}}" answers {{.Method}} {{.Path}}.

{{.AccessLine}}

{{.InputIntro}}
{{range .BodyInput}}- {{.Line}}
{{end}}{{if .InputRule}}{{.InputRule}}
{{end}}{{if .ServerSet}}
{{.ServerIntro}}
{{range .ServerSet}}- {{.Line}}
{{end}}{{end}}
Every answer is JSON. An error answer is ` + "`{{.ErrorShape}}`" + `.
- {{.SuccessLine}}
{{range .Output}}  - {{.Line}}
{{end}}{{range .Answers}}- {{.}}
{{end}}
{{.DataLine}}

## Preconditions

{{if .Pre}}Checked first, before step 1. A false one is a bug, not a user error: the request stops with HTTP {{.InternalStatus}}.
{{range .Pre}}- {{.}}
{{end}}{{else}}{{.NoPre}}
{{end}}
## Steps

The action does exactly this, in this order. A step that stops the action ends it: no later step runs.

{{if .TxLine}}{{.TxLine}}

{{end}}{{range .Steps}}{{.N}}. {{.Text}}
{{range .Bullets}}   - {{.}}
{{end}}{{range .Notes}}   {{.}}
{{end}}{{end}}
## Failure cases

Index by F-ID; the order they are checked in is in Steps.
{{range .Failures}}- {{.Line}}
{{end}}`
