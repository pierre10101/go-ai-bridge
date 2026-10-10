package adapter

import (
	"bufio"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SQL is read with a small tokenizer and a recursive-descent parser that
// accepts only the Q-patterns in Grammar. There is no "best effort": any
// token outside a recognised shape is refused with file:line:col, exactly
// like Go constructs in action.go.

// SQLQuery is one sqlc-annotated query in queries/*.sql, parsed into a shape.
type SQLQuery struct {
	Name, Cmd, File string
	Shape           string // "count" (Q1), "row" (Q2), "insert" (Q3), "page" (Q5) or "claim" (Q6, and Q8, Q10)
	Table           string
	// Delete marks a Q10 delete: DELETE FROM <Table> WHERE <Conds>. It is a
	// claim with no SET: it names its rows by the table's key, checks its
	// conditions in the statement that deletes, and S10 (or S11) checks how
	// many rows it removed. OnDelete is what schema.sql does to the rows
	// that reference a deleted row (checkDeletes), for the English.
	Delete   bool
	OnDelete []fkEffect
	// Source is, for a Q8 insert from a parent row (INSERT INTO <Table> (...)
	// SELECT ... FROM <Source> WHERE ...), the parent table; Conds are then
	// that SELECT's WHERE conditions (on Source). Without RETURNING a Q8 is a
	// claim (:execrows, S10). With RETURNING it is Shape "insert" (:one; no
	// row is sql.ErrNoRows, checked by S10).
	Source string
	Where  []sqlAssign // Q1, Q2, Q5: <col> = <value> (Q1, Q2: or <col> <op> <the current time>), joined by AND
	Values []sqlAssign // Q3: column = value, in column order; Q6: SET column = value
	Conds  []sqlCond   // Q6: WHERE conditions joined by AND (one may be an OR group)
	Cols   []string    // Q2/Q5: selected columns; Q3/Q8+:one: RETURNING columns
	Params []string    // distinct parameter names, in order of appearance
	// Q5 keyset page:
	CursorCol     string // ORDER BY column; WHERE CursorCol < CursorVal
	CursorVal     sqlVal
	Limit         sqlVal // LIMIT value (param or int literal 1..page.MaxPageSize)
	LimitN        int    // LIMIT literal, checked against page.MaxPageSize when rendered
	JoinTable     string // Q5 with one INNER JOIN: the parent table
	JoinFK        string // child column of ON child.fk = parent.pk
	JoinPK        string // parent column of that equijoin (parent's PRIMARY KEY)
	Reads, Writes []string
	// Q7 IN list: <SliceCol> IN (sqlc.slice(<Slice>)), at most one per query.
	Slice, SliceCol string
	slicePos        token.Position
	// Subs lists every Q9 proof subquery of the statement, outermost first.
	Subs []*sqlSub
	bad  bool // refused; already reported
	pos  token.Position
	toks []sqlTok
}

type sqlAssign struct {
	Col string
	Op  string // Q1, Q2 WHERE: "=" or, against the server-set current time only, "<>", "<", "<=", ">", ">="; "sub": Col IN (Sub) (Q9)
	Val sqlVal
	Sub *sqlSub
	pos token.Position
}

// sqlSub is a Q9 proof subquery, Col IN (SELECT <Table>.<Key> FROM <Table>
// WHERE <Conds>): the rows of <Table> whose key the outer column holds. A5
// accepts it only as the proof that a child row's parent is the signed-in
// user's (checkProofs): Conds is exactly <Table>.<owner col> = <value>, or,
// when <Table> is a child itself, <Table>.<parent col> IN (<sqlSub>).
type sqlSub struct {
	Outer, OuterCol string // the table and column the subquery is compared with
	Table, Key      string
	Conds           []sqlCond
	pos             token.Position // the outer column
}

// clockCond reports whether a Q1/Q2 WHERE condition is more than <col> =
// <value>: another comparison or an offset. Such a condition is allowed only
// against the action's server-set clock input (T1), which the walker checks
// where the query is called (readClockHint).
func (a sqlAssign) clockCond() bool {
	return a.Val.Kind == "param" && (a.Op != "=" || a.Val.Op != "")
}

// readClockHint is attached to every refusal of a comparison in a read.
const readClockHint = "In a Q1 count or Q2 one-row read, a column is compared with <> < <= > >= or with an offset only against the action's server-set current time (T1): <col> <op> sqlc.arg(now) [+ or - <seconds>], the column first, with the action passing in.Now (an Input field tagged clock:\"now\") for it; every other condition is <col> = <value>"

type sqlVal struct {
	Kind  string // "param", "int", "string", "next", "slice" (Q7: sqlc.slice(<Param>)), "col" (Q8: the parent's column Lit)
	Param string // Kind param: sqlc parameter name
	Lit   string // Kind int/string: literal text
	Op    string // Q6 only: "+" or "-" with Off (a parameter plus or minus a whole number)
	Off   string
}

// sqlCond is one Q6 WHERE condition: <col> <op> <value>, or a parenthesised
// OR group of such conditions (Any).
type sqlCond struct {
	Col, Op string // Op "in": Col IN (sqlc.slice(Val.Param)) (Q7); "sub": Col IN (Sub) (Q9)
	Val     sqlVal
	Any     []sqlCond
	Sub     *sqlSub
	pos     token.Position
}

type sqlTok struct {
	kind string // "word", "qword" (quoted identifier), "num", "str", "param", "punct", "eof"
	text string
	up   string
	pos  token.Position
}

var nameRe = regexp.MustCompile(`^--\s*name:\s*(\w+)\s+(:\w+)`)

// allowedSQL is the hint attached to every SQL refusal.
const allowedSQL = "allowed SQL shapes: Q1 count, Q2 one row, Q3 insert, Q5 keyset page, Q6 claim update, Q7 IN list, Q8 insert from a parent row, Q9 proof subquery, Q10 delete (see bridge-en -grammar)"

// LoadQueries reads every queries/*.sql file in dir, keyed by query name.
// Unrecognised SQL is returned as Refusals; refused queries are still in the
// map (marked bad) so callers do not report them twice.
func LoadQueries(dir string) (map[string]*SQLQuery, Refusals, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	out := map[string]*SQLQuery{}
	var errs Refusals
	for _, path := range files {
		if err := parseSQLFile(path, out, &errs); err != nil {
			return nil, nil, err
		}
	}
	return out, errs, nil
}

func parseSQLFile(path string, out map[string]*SQLQuery, errs *Refusals) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	var cur *SQLQuery
	flush := func() {
		if cur == nil {
			return
		}
		if !cur.bad {
			p := &sqlParser{q: cur, toks: cur.toks, errs: errs}
			p.statement()
			cur.bad = p.failed
			if !cur.bad {
				switch {
				case cur.Cmd == ":many" && cur.Shape != "page":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation :many on a non-page shape", Context: "Q0 query annotation",
						Hint: ":many is only for Q5 keyset pages (WHERE … AND <cursor> < ? ORDER BY <cursor> DESC LIMIT n)"})
					cur.bad = true
				case cur.Cmd == ":one" && cur.Shape == "page":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation :one on a keyset page", Context: "Q0 query annotation",
						Hint: "Q5 keyset pages use :many"})
					cur.bad = true
				case cur.Source != "" && len(cur.Cols) > 0 && cur.Cmd != ":one":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation " + cur.Cmd + " on an insert from a parent row with RETURNING", Context: "Q0 query annotation",
						Hint: "A Q8 insert from a parent row with RETURNING answers with the stored row: annotate it :one; no row is sql.ErrNoRows (S10)"})
					cur.bad = true
				case cur.Source != "" && len(cur.Cols) == 0 && cur.Cmd != ":execrows":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation " + cur.Cmd + " on an insert from a parent row", Context: "Q0 query annotation",
						Hint: "A Q8 insert from a parent row without RETURNING adds one row or none: annotate it :execrows and check that number is exactly 1 (S10); with RETURNING use :one"})
					cur.bad = true
				case cur.Cmd != ":execrows" && cur.Delete:
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation " + cur.Cmd + " on a delete", Context: "Q0 query annotation",
						Hint: "A Q10 delete answers with the number of rows it removed: annotate it :execrows and check that number is exactly 1 (S10)"})
					cur.bad = true
				case cur.Cmd != ":execrows" && cur.Shape == "claim":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation " + cur.Cmd + " on a claim update", Context: "Q0 query annotation",
						Hint: "A Q6 claim answers with the number of rows it changed: annotate it :execrows and check that number is exactly 1 (S10)"})
					cur.bad = true
				case cur.Cmd == ":execrows" && cur.Shape != "claim":
					*errs = append(*errs, Refusal{Pos: cur.pos, Construct: "query annotation :execrows on a non-claim shape", Context: "Q0 query annotation",
						Hint: ":execrows is only for Q6 claim updates, Q8 inserts from a parent row without RETURNING, and Q10 deletes"})
					cur.bad = true
				}
			}
		}
		cur.toks = nil
		out[cur.Name] = cur
	}
	sc := bufio.NewScanner(fh)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "--") {
			m := nameRe.FindStringSubmatch(line)
			if m == nil {
				continue // plain comment
			}
			flush()
			pos := token.Position{Filename: path, Line: lineNo, Column: strings.Index(raw, "--") + 1}
			cur = &SQLQuery{Name: m[1], Cmd: m[2], File: "queries/" + filepath.Base(path), pos: pos}
			if out[cur.Name] != nil {
				*errs = append(*errs, Refusal{Pos: pos, Construct: "second query named " + cur.Name, Context: "Q0 query annotation", Hint: "Query names are unique"})
				cur.bad = true
			}
			if cur.Cmd != ":one" && cur.Cmd != ":many" && cur.Cmd != ":execrows" {
				*errs = append(*errs, Refusal{Pos: pos, Construct: "query annotation " + cur.Cmd, Context: "Q0 query annotation",
					Hint: "Only :one (Q1-Q3), :many (Q5 keyset page) or :execrows (Q6 claim update, Q8 insert from a parent row, Q10 delete) are in the grammar"})
				cur.bad = true
			}
			continue
		}
		toks := tokenizeSQL(raw, path, lineNo)
		if len(toks) == 0 {
			continue
		}
		if cur == nil {
			*errs = append(*errs, Refusal{Pos: toks[0].pos, Construct: "SQL before any -- name: annotation", Context: "Q0 query annotation",
				Hint: "Start every query with -- name: <Query> :one, :many or :execrows"})
			return nil
		}
		cur.toks = append(cur.toks, toks...)
	}
	flush()
	return sc.Err()
}

func isWordStart(c byte) bool { return c == '_' || (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func isDigit(c byte) bool     { return c >= '0' && c <= '9' }

// tokenizeSQL splits one line. Everything after -- is a comment.
func tokenizeSQL(line, file string, lineNo int) []sqlTok {
	var out []sqlTok
	i := 0
	for i < len(line) {
		c := line[i]
		start := i
		pos := token.Position{Filename: file, Line: lineNo, Column: i + 1}
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case c == '-' && i+1 < len(line) && line[i+1] == '-':
			return out
		case isWordStart(c):
			for i < len(line) && (isWordStart(line[i]) || isDigit(line[i])) {
				i++
			}
			w := line[start:i]
			out = append(out, sqlTok{kind: "word", text: w, up: strings.ToUpper(w), pos: pos})
			continue
		case isDigit(c):
			for i < len(line) && isDigit(line[i]) {
				i++
			}
			out = append(out, sqlTok{kind: "num", text: line[start:i], up: line[start:i], pos: pos})
			continue
		case c == '\'':
			i++
			for i < len(line) {
				if line[i] == '\'' {
					if i+1 < len(line) && line[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			if i < len(line) {
				i++
			}
			out = append(out, sqlTok{kind: "str", text: line[start:i], up: line[start:i], pos: pos})
			continue
		case c == '"' || c == '`':
			i++
			for i < len(line) && line[i] != c {
				i++
			}
			name := line[start+1 : i]
			if i < len(line) {
				i++
			}
			out = append(out, sqlTok{kind: "qword", text: name, up: strings.ToUpper(name), pos: pos})
			continue
		case c == '?':
			i++
			for i < len(line) && isDigit(line[i]) {
				i++
			}
			out = append(out, sqlTok{kind: "param", text: line[start:i], up: line[start:i], pos: pos})
			continue
		}
		i++
		if i < len(line) {
			switch two := line[start : i+1]; two {
			case "<=", ">=", "<>", "!=", "==", "||":
				i++
			}
		}
		out = append(out, sqlTok{kind: "punct", text: line[start:i], up: line[start:i], pos: pos})
	}
	return out
}

// reserved words are never table or column names.
var reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`SELECT FROM WHERE AND OR NOT INSERT INTO VALUES RETURNING UPDATE DELETE SET
		JOIN LEFT RIGHT INNER OUTER CROSS NATURAL FULL ON USING UNION INTERSECT EXCEPT GROUP ORDER BY HAVING LIMIT
		OFFSET DISTINCT ALL AS WITH REPLACE IN IS NULL LIKE GLOB BETWEEN EXISTS CASE WHEN THEN ELSE END CONFLICT
		DO NOTHING ABORT IGNORE FAIL ROLLBACK DEFAULT COUNT MAX MIN COALESCE`) {
		reserved[w] = true
	}
}

// sqlConstructs names well-known SQL that is outside the grammar.
var sqlConstructs = map[string]string{
	"JOIN": "JOIN", "LEFT": "JOIN", "RIGHT": "JOIN", "INNER": "JOIN", "OUTER": "JOIN", "CROSS": "JOIN",
	"NATURAL": "JOIN", "FULL": "JOIN", "UNION": "UNION", "INTERSECT": "INTERSECT", "EXCEPT": "EXCEPT",
	"GROUP": "GROUP BY", "ORDER": "ORDER BY", "HAVING": "HAVING", "LIMIT": "LIMIT", "OFFSET": "OFFSET",
	"DISTINCT": "SELECT DISTINCT", "WITH": "WITH (common table expression)", "REPLACE": "REPLACE statement",
	"UPDATE": "UPDATE statement", "DELETE": "DELETE statement", "AS": "alias (AS)", "NULL": "NULL",
	"IN": "IN", "LIKE": "LIKE", "GLOB": "GLOB", "BETWEEN": "BETWEEN", "EXISTS": "EXISTS", "CASE": "CASE",
	"IS": "IS", "NOT": "NOT", "DEFAULT": "DEFAULT VALUES",
}

type sqlParser struct {
	q          *SQLQuery
	toks       []sqlTok
	i          int
	errs       *Refusals
	failed     bool
	uses       []paramUse  // every parameter in statement order (Q7: the slice is last)
	bare       []sqlTok    // column names written without their table (Q8, Q9 need it)
	selectQual []selectCol // SELECT list with optional table (Q5 JOIN)
}

// paramUse is one parameter in the statement text.
type paramUse struct {
	name  string
	slice bool
	pos   token.Position
}

func (p *sqlParser) peekAt(k int) sqlTok {
	if p.i+k < len(p.toks) {
		return p.toks[p.i+k]
	}
	pos := p.q.pos
	if n := len(p.toks); n > 0 {
		pos = p.toks[n-1].pos
		pos.Column += len(p.toks[n-1].text)
	}
	return sqlTok{kind: "eof", text: "end of query", pos: pos}
}

func (p *sqlParser) peek() sqlTok { return p.peekAt(0) }

// is reports whether the next token is the keyword or punctuation s.
func (p *sqlParser) is(s string) bool {
	t := p.peek()
	return (t.kind == "word" || t.kind == "punct" || t.kind == "num") && t.up == s
}

func (p *sqlParser) accept(s string) bool {
	if p.failed || !p.is(s) {
		return false
	}
	p.i++
	return true
}

func (p *sqlParser) need(s, expected string) bool {
	if p.accept(s) {
		return true
	}
	p.fail(expected)
	return false
}

func (p *sqlParser) fail(expected string) {
	if p.failed {
		return
	}
	p.failed = true
	t := p.peek()
	*p.errs = append(*p.errs, Refusal{Pos: t.pos, Construct: p.construct(t), Context: "query " + p.q.Name,
		Hint: "Expected " + expected + "; " + allowedSQL})
}

// construct names the refused SQL so the reader recognises it.
func (p *sqlParser) construct(t sqlTok) string {
	prev, next := "", p.peekAt(1)
	if p.i > 0 {
		prev = p.toks[p.i-1].up
	}
	switch {
	case t.kind == "eof" || t.up == ";":
		return "end of statement"
	case t.up == "RETURNING" && p.q.Delete:
		return "RETURNING on a DELETE"
	case t.up == "RETURNING" && p.q.Shape == "claim":
		return "RETURNING on an UPDATE"
	case t.up == "OR" && prev == "INSERT":
		return "INSERT OR " + next.up
	case t.up == "OR":
		return "OR in WHERE"
	case t.up == "ON" && next.up == "CONFLICT":
		return "ON CONFLICT (upsert)"
	case t.up == "*" && prev == "SELECT":
		return "SELECT *"
	case t.up == "(" && next.up == "SELECT":
		return "subquery"
	case t.up == "," && prev != "" && p.inFrom():
		return "FROM with several tables"
	case p.i > 0 && prev == ";":
		return "second statement in one query"
	case t.kind == "word":
		if name, ok := sqlConstructs[t.up]; ok {
			return name
		}
		if p.i == 0 {
			return t.up + " statement"
		}
		return fmt.Sprintf("SQL %q", t.text)
	}
	return fmt.Sprintf("SQL %q", t.text)
}

func (p *sqlParser) inFrom() bool { return p.i >= 2 && p.toks[p.i-2].up == "FROM" }

// ident reads a table or column name.
func (p *sqlParser) ident(expected string) (string, bool) {
	if p.failed {
		return "", false
	}
	t := p.peek()
	if t.kind == "qword" || (t.kind == "word" && !reserved[t.up]) {
		p.i++
		return strings.ToLower(t.text), true
	}
	p.fail(expected)
	return "", false
}

// argName reads a sqlc.arg(...) parameter name; reserved words like limit are allowed.
func (p *sqlParser) argName() (string, bool) {
	if p.failed {
		return "", false
	}
	t := p.peek()
	if t.kind == "qword" || t.kind == "word" {
		p.i++
		return strings.ToLower(t.text), true
	}
	p.fail("a parameter name")
	return "", false
}

func (p *sqlParser) addParam(name string) {
	for _, n := range p.q.Params {
		if n == name {
			return
		}
	}
	p.q.Params = append(p.q.Params, name)
}

// statement parses exactly one Q1, Q2 or Q3 statement and the end of the query.
func (p *sqlParser) statement() {
	switch {
	case p.accept("SELECT"):
		if p.accept("COUNT") {
			p.count()
		} else {
			p.row()
		}
	case p.accept("INSERT"):
		p.insert()
	case p.accept("UPDATE"):
		p.update()
	case p.accept("DELETE"):
		p.del()
	default:
		p.fail("SELECT, INSERT, UPDATE or DELETE")
	}
	if !p.failed && p.q.Delete && p.is("RETURNING") {
		p.fail("end of query (a Q10 delete gives back only the number of rows it removed: annotate :execrows, no RETURNING, and check the count with != 1, S10)")
	}
	if !p.failed && p.q.Shape == "claim" && p.q.Source == "" && p.is("RETURNING") {
		p.fail("end of query (a Q6 claim gives back only the number of rows it changed: annotate :execrows, no RETURNING)")
	}
	p.accept(";")
	if !p.failed && p.peek().kind != "eof" {
		if p.i > 0 && p.toks[p.i-1].up == ";" {
			p.fail("end of query (one statement per query)")
		} else {
			p.fail("end of query")
		}
	}
	if p.failed {
		return
	}
	p.sliceLast()
	if p.failed {
		return
	}
	if (len(p.q.Subs) > 0 || p.q.Source != "") && len(p.bare) > 0 {
		b := p.bare[0]
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: b.pos, Construct: "column " + b.text + " without its table in a query that reads two tables", Context: "query " + p.q.Name,
			Hint: "With a Q8 insert from a parent row or a Q9 subquery the statement names two tables, and sqlc cannot tell their columns apart (both have id): write every column of a WHERE and of the SELECT list as <table>.<col>, for example " + p.q.Table + "." + b.text + " (the INSERT column list and SET stay plain)"})
		return
	}
	var subTables []string
	for _, sub := range p.q.Subs {
		subTables = appendUnique(subTables, sub.Table)
	}
	if p.q.JoinTable != "" && p.q.Shape != "page" {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: p.q.pos, Construct: "JOIN in a non-page SELECT", Context: "query " + p.q.Name,
			Hint: "A JOIN is only for a Q5 keyset page (WHERE … AND <cursor> < ? ORDER BY <cursor> DESC LIMIT n); " + allowedSQL})
		return
	}
	if p.q.Shape == "page" {
		p.checkPageSelect()
		if p.failed {
			return
		}
		if p.q.JoinTable != "" && len(p.bare) > 0 {
			b := p.bare[0]
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: b.pos, Construct: "column " + b.text + " without its table in a keyset page that joins two tables", Context: "query " + p.q.Name,
				Hint: "Write every column as " + p.q.Table + ".<col> or " + p.q.JoinTable + ".<col>; " + allowedSQL})
			return
		}
	}
	switch p.q.Shape {
	case "count", "row", "page":
		p.q.Reads = append([]string{p.q.Table}, subTables...)
		if p.q.JoinTable != "" {
			p.q.Reads = appendUnique(p.q.Reads, p.q.JoinTable)
		}
	case "claim":
		// The WHERE reads the row in the same statement that writes it.
		// A delete's cascades are added by checkDeletes (schema.sql).
		p.q.Reads, p.q.Writes = append([]string{p.q.Table}, subTables...), []string{p.q.Table}
		if p.q.Source != "" { // Q8: reads the parent row, adds a row to Table
			p.q.Reads = []string{p.q.Source}
			for _, t := range subTables {
				p.q.Reads = appendUnique(p.q.Reads, t)
			}
		}
	case "insert":
		p.q.Writes = []string{p.q.Table}
		if p.q.Source != "" { // Q8 with RETURNING: reads the parent, writes the child
			p.q.Reads = []string{p.q.Source}
			for _, t := range subTables {
				p.q.Reads = appendUnique(p.q.Reads, t)
			}
			break
		}
		for _, v := range p.q.Values {
			if v.Val.Kind == "next" {
				p.q.Reads = []string{p.q.Table}
			}
		}
	}
}

// Q1: SELECT COUNT(*) FROM <table> WHERE <conds>
func (p *sqlParser) count() {
	p.q.Shape = "count"
	_ = p.need("(", "( after COUNT") && p.need("*", "* in COUNT(*)") && p.need(")", ") after COUNT(*") &&
		p.need("FROM", "FROM after COUNT(*)")
	p.from()
}

// Q2: SELECT <col>, ... FROM <table> WHERE <conds>
// Q5 may qualify columns and add one INNER JOIN (see fromJoin).
func (p *sqlParser) row() {
	p.q.Shape = "row"
	for {
		at := p.peek()
		qual, ok := p.ident("a column name")
		if !ok {
			return
		}
		col := qual
		if p.accept(".") {
			tbl := qual
			if col, ok = p.ident("a column name after " + tbl + "."); !ok {
				return
			}
			p.q.Cols = append(p.q.Cols, tbl+"."+col)
			p.selectQual = append(p.selectQual, selectCol{table: tbl, col: col, at: at})
		} else {
			p.q.Cols = append(p.q.Cols, col)
			p.bare = append(p.bare, at)
			p.selectQual = append(p.selectQual, selectCol{col: col, at: at})
		}
		if !p.accept(",") {
			break
		}
	}
	if p.need("FROM", ", or FROM after the column list") {
		p.from()
	}
}

type selectCol struct {
	table, col string
	at         sqlTok
}

func (p *sqlParser) from() {
	table, ok := p.ident("a table name")
	if !ok {
		return
	}
	p.q.Table = table
	p.fromJoin()
	if p.failed {
		return
	}
	if !p.need("WHERE", "WHERE after FROM "+table) {
		return
	}
	for {
		at := p.peek()
		if at.kind == "word" && at.up == "SQLC" || at.kind == "param" {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "parameter on the left of a condition", Context: "query " + p.q.Name,
				Hint: readClockHint + "; " + allowedSQL})
			return
		}
		col, ok := p.pageColRef("a column name in WHERE")
		if !ok {
			return
		}
		if p.is("IN") && p.peekAt(1).up == "(" && p.peekAt(2).up == "SELECT" {
			sub, ok := p.sub(table, col, at)
			if !ok {
				return
			}
			p.q.Where = append(p.q.Where, sqlAssign{Col: col, Op: "sub", Sub: sub, pos: at.pos})
		} else if p.is("IN") {
			v, ok := p.inSlice(col)
			if !ok {
				return
			}
			p.q.Where = append(p.q.Where, sqlAssign{Col: col, Op: "=", Val: v, pos: at.pos})
		} else {
			t := p.peek()
			op, isOp := claimOps[t.up]
			if t.kind != "punct" || !isOp {
				p.fail("= (or a comparison <> < <= > >= with the current time) after " + col)
				return
			}
			p.i++
			v, ok := p.value(col, false)
			if !ok {
				return
			}
			// Q5 keyset: <cursor> < <value> ORDER BY <cursor> DESC (newest first).
			// Equality filters before the cursor are optional: a public catalog
			// page may be only the cursor (WHERE id < ? ORDER BY id DESC LIMIT ?).
			if op == "<" && p.q.Shape == "row" && p.is("ORDER") {
				p.q.CursorCol, p.q.CursorVal = col, v
				p.finishPage()
				p.noComparisonInPage()
				return
			}
			if v, ok = p.offset(v); !ok {
				return
			}
			c := sqlAssign{Col: col, Op: op, Val: v, pos: at.pos}
			if (op != "=" || v.Op != "") && v.Kind != "param" {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "comparison " + col + " " + op + " " + v.Lit + " in a read", Context: "query " + p.q.Name,
					Hint: readClockHint})
				return
			}
			p.q.Where = append(p.q.Where, c)
		}
		if !p.accept("AND") {
			return
		}
	}
}

// fromJoin reads an optional single INNER JOIN for a Q5 page:
// [INNER] JOIN <parent> ON <child>.<fk> = <parent>.<pk>.
func (p *sqlParser) fromJoin() {
	at := p.peek()
	switch at.up {
	case "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL", "OUTER":
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: at.up + " JOIN", Context: "query " + p.q.Name,
			Hint: "A Q5 keyset page may use one INNER JOIN (JOIN or INNER JOIN) equijoining the child to the parent's primary key; OUTER/LEFT/RIGHT/CROSS joins are refused; " + allowedSQL})
		return
	case "INNER":
		p.i++
		if !p.need("JOIN", "JOIN after INNER") {
			return
		}
	case "JOIN":
		p.i++
	default:
		return
	}
	parent, ok := p.ident("the joined parent table")
	if !ok {
		return
	}
	if !p.need("ON", "ON after JOIN "+parent) {
		return
	}
	// ON child.fk = parent.pk (either side order).
	leftAt := p.peek()
	leftTbl, ok := p.ident("a table name in the JOIN ON")
	if !ok || !p.need(".", ".") {
		return
	}
	leftCol, ok := p.ident("a column name after " + leftTbl + ".")
	if !ok || !p.need("=", "= in the JOIN ON equijoin") {
		return
	}
	rightAt := p.peek()
	rightTbl, ok := p.ident("a table name in the JOIN ON")
	if !ok || !p.need(".", ".") {
		return
	}
	rightCol, ok := p.ident("a column name after " + rightTbl + ".")
	if !ok {
		return
	}
	child, parentTbl := p.q.Table, parent
	var fk, pk string
	switch {
	case leftTbl == child && rightTbl == parentTbl:
		fk, pk = leftCol, rightCol
	case leftTbl == parentTbl && rightTbl == child:
		fk, pk = rightCol, leftCol
	default:
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: leftAt.pos, Construct: "JOIN ON " + leftTbl + "." + leftCol + " = " + rightTbl + "." + rightCol, Context: "query " + p.q.Name,
			Hint: "The equijoin names the child and the joined parent: " + child + ".<fk> = " + parentTbl + ".<pk>; " + allowedSQL})
		_ = rightAt
		return
	}
	p.q.JoinTable, p.q.JoinFK, p.q.JoinPK = parentTbl, fk, pk
	// A second JOIN is refused.
	if p.is("JOIN") || p.is("INNER") || p.is("LEFT") || p.is("RIGHT") || p.is("FULL") || p.is("CROSS") || p.is("NATURAL") || p.is("OUTER") {
		p.fail("WHERE after the one JOIN (a Q5 page has at most one INNER JOIN)")
	}
}

// noComparisonInPage refuses a comparison with the current time in a Q5
// keyset page: a page compares only its cursor.
func (p *sqlParser) noComparisonInPage() {
	if p.failed {
		return
	}
	for _, c := range p.q.Where {
		if c.clockCond() {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: c.pos, Construct: "comparison " + c.Col + " " + c.Op + " " + sqlParamText(c.Val) + " in a keyset page", Context: "query " + p.q.Name,
				Hint: "A Q5 keyset page compares only its cursor (<cursor> < <value>); every other condition is <col> = <value>. A comparison with the current time belongs in a Q1 count, a Q2 one-row read or a Q6 claim"})
			return
		}
	}
}

// finishPage reads ORDER BY <cursor> DESC LIMIT <n> for Q5.
func (p *sqlParser) finishPage() {
	p.q.Shape = "page"
	if !p.need("ORDER", "ORDER BY "+p.q.CursorCol+" DESC after the keyset WHERE") ||
		!p.need("BY", "BY after ORDER") {
		return
	}
	at := p.peek()
	col, ok := p.pageColRef("the cursor column " + p.q.CursorCol)
	if !ok {
		return
	}
	if col != p.q.CursorCol {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "ORDER BY " + col, Context: "query " + p.q.Name,
			Hint: "ORDER BY must use the same column as the keyset cursor (" + p.q.CursorCol + "); " + allowedSQL})
		return
	}
	if !p.need("DESC", "DESC after ORDER BY "+col+" (newest first)") {
		return
	}
	if p.is("OFFSET") {
		p.fail("LIMIT (OFFSET is not in the grammar; use keyset cursors)")
		return
	}
	if !p.need("LIMIT", "LIMIT after ORDER BY (a list without LIMIT is refused)") {
		return
	}
	at = p.peek()
	v, ok := p.value("limit", false)
	if !ok {
		return
	}
	if v.Kind == "int" {
		n := 0
		for _, c := range v.Lit {
			if c < '0' || c > '9' {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "LIMIT " + v.Lit, Context: "query " + p.q.Name,
					Hint: "LIMIT literal must be a positive integer (at most page.MaxPageSize); " + allowedSQL})
				return
			}
			n = n*10 + int(c-'0')
		}
		if n < 1 {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: fmt.Sprintf("LIMIT %d", n), Context: "query " + p.q.Name,
				Hint: "LIMIT must be 1..page.MaxPageSize; " + allowedSQL})
			return
		}
		p.q.LimitN = n
	} else if v.Kind != "param" {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "LIMIT that is not a parameter or integer", Context: "query " + p.q.Name,
			Hint: allowedSQL})
		return
	}
	p.q.Limit = v
	if p.is("OFFSET") {
		p.fail("end of query (OFFSET is not in the grammar; use keyset cursors)")
	}
}

// Q3: INSERT INTO <table> (<col>, ...) VALUES (<value>, ...) RETURNING <col>, ...
func (p *sqlParser) insert() {
	p.q.Shape = "insert"
	if !p.need("INTO", "INTO after INSERT") {
		return
	}
	table, ok := p.ident("a table name")
	if !ok || !p.need("(", "( and the column list") {
		return
	}
	p.q.Table = table
	var cols []string
	for {
		col, ok := p.ident("a column name")
		if !ok {
			return
		}
		cols = append(cols, col)
		if !p.accept(",") {
			break
		}
	}
	if !p.need(")", ", or ) in the column list") {
		return
	}
	if p.accept("SELECT") {
		p.insertSelect(cols)
		return
	}
	if !p.need("VALUES", "VALUES (or SELECT, Q8)") {
		return
	}
	open := p.peek()
	if !p.need("(", "( and the values") {
		return
	}
	for i := 0; ; i++ {
		if i >= len(cols) {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: open.pos, Construct: fmt.Sprintf("VALUES with more values than the %d columns", len(cols)),
				Context: "query " + p.q.Name, Hint: "One value per column; " + allowedSQL})
			return
		}
		v, ok := p.value(cols[i], true)
		if !ok {
			return
		}
		p.q.Values = append(p.q.Values, sqlAssign{Col: cols[i], Val: v})
		if !p.accept(",") {
			break
		}
	}
	if len(p.q.Values) != len(cols) {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: open.pos, Construct: fmt.Sprintf("VALUES with %d values for %d columns", len(p.q.Values), len(cols)),
			Context: "query " + p.q.Name, Hint: "One value per column; " + allowedSQL})
		return
	}
	if !p.need(")", ", or ) in the values") {
		return
	}
	if !p.need("RETURNING", "RETURNING <col>, ... (a :one insert gives back the stored row)") {
		return
	}
	for {
		col, ok := p.ident("a column name after RETURNING")
		if !ok {
			return
		}
		p.q.Cols = append(p.q.Cols, col)
		if !p.accept(",") {
			return
		}
	}
}

// Q6: UPDATE <table> SET <col> = <value>, ... WHERE <cond> [AND <cond>]...
// where <cond> is <col> <op> <value> or one parenthesised (<cond> OR <cond> ...).
// The condition is checked by the statement that writes: a claim, never a
// read followed by a write (W1). At least one condition is <col> = <parameter>
// (which rows), and there is no RETURNING (:execrows, checked by S10).
func (p *sqlParser) update() {
	p.q.Shape = "claim"
	table, ok := p.ident("a table name")
	if !ok {
		return
	}
	p.q.Table = table
	if !p.need("SET", "SET after UPDATE "+table) {
		return
	}
	for {
		col, ok := p.ident("a column name after SET")
		if !ok || !p.need("=", "= after "+col) {
			return
		}
		v, ok := p.claimValue(col)
		if !ok {
			return
		}
		p.q.Values = append(p.q.Values, sqlAssign{Col: col, Val: v})
		if !p.accept(",") {
			break
		}
	}
	if !p.need("WHERE", "WHERE after SET (a claim names its rows and its condition; an UPDATE without WHERE changes every row)") {
		return
	}
	group := false
	for {
		if p.is("(") && p.peekAt(1).up != "SELECT" {
			at := p.peek()
			p.i++
			if group {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "second OR group", Context: "query " + p.q.Name,
					Hint: "A Q6 claim has at most one parenthesised (<cond> OR <cond> ...) group; " + allowedSQL})
				return
			}
			group = true
			var any []sqlCond
			for {
				c, ok := p.cond(table, true)
				if !ok {
					return
				}
				any = append(any, c)
				if !p.accept("OR") {
					break
				}
			}
			if !p.need(")", "OR or ) closing the group") {
				return
			}
			if len(any) < 2 {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "parentheses around one condition", Context: "query " + p.q.Name,
					Hint: "Parentheses in a Q6 WHERE hold one OR group: (<cond> OR <cond> ...); " + allowedSQL})
				return
			}
			p.q.Conds = append(p.q.Conds, sqlCond{Any: any})
		} else {
			c, ok := p.cond(table, false)
			if !ok {
				return
			}
			p.q.Conds = append(p.q.Conds, c)
		}
		if !p.accept("AND") {
			break
		}
	}
	for _, c := range p.q.Conds {
		if (c.Op == "=" && c.Val.Kind == "param" && c.Val.Op == "") || c.Op == "in" {
			return
		}
	}
	p.failed = true
	*p.errs = append(*p.errs, Refusal{Pos: p.q.pos, Construct: "claim update with no <col> = <parameter> condition", Context: "query " + p.q.Name,
		Hint: "A Q6 claim names the rows it may change with <col> = ? (for example id = ?), or with <key> IN (sqlc.slice(<name>)) (Q7), outside any OR group; " + allowedSQL})
}

// Q10: DELETE FROM <table> WHERE <cond> [AND <cond>]... where <cond> is
// <col> = <value>, <key> IN (sqlc.slice(<name>)) (Q7) or a Q9 proof
// subquery: no OR, no other comparison, no RETURNING. It is a claim with no
// SET (:execrows, checked by S10 or S11); checkClaimKeys requires one
// condition to name the rows by the table's single-column PRIMARY KEY, and
// checkDeletes what schema.sql does to the rows that reference them.
func (p *sqlParser) del() {
	p.q.Shape, p.q.Delete = "claim", true
	if !p.need("FROM", "FROM after DELETE") {
		return
	}
	table, ok := p.ident("a table name")
	if !ok {
		return
	}
	p.q.Table = table
	if t := p.peek(); !p.is("WHERE") && (t.kind == "eof" || t.up == ";") {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: t.pos, Construct: "DELETE without WHERE", Context: "query " + p.q.Name,
			Hint: "A DELETE without WHERE removes every row of " + table + ". A Q10 delete names its rows by the table's key: DELETE FROM " + table + " WHERE <key> = sqlc.arg(<key>) [AND <col> = <value>]... (or <key> IN (sqlc.slice(<name>)), Q7)"})
		return
	}
	if !p.need("WHERE", "WHERE after DELETE FROM "+table+" (a delete names its rows by their key)") {
		return
	}
	for {
		at := p.peek()
		if p.is("(") && p.peekAt(1).up != "SELECT" {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "parenthesised condition in a delete", Context: "query " + p.q.Name,
				Hint: "A Q10 delete joins its conditions with AND only (no OR group): <col> = <value>, <key> IN (sqlc.slice(<name>)) or the Q9 proof subquery"})
			return
		}
		c, ok := p.cond(table, false)
		if !ok {
			return
		}
		if c.Op != "=" && c.Op != "in" && c.Op != "sub" || c.Val.Op != "" {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "comparison " + c.Col + " " + c.Op + " " + sqlValText(c.Val) + " in a delete", Context: "query " + p.q.Name,
				Hint: "A Q10 delete compares only with = (and the key with IN (sqlc.slice(<name>)), Q7, or the Q9 proof subquery); a condition on time or ranges belongs in a Q6 claim that marks the row instead"})
			return
		}
		p.q.Conds = append(p.q.Conds, c)
		if !p.accept("AND") {
			break
		}
	}
	for _, c := range p.q.Conds {
		if (c.Op == "=" && c.Val.Kind == "param") || c.Op == "in" {
			return
		}
	}
	p.failed = true
	*p.errs = append(*p.errs, Refusal{Pos: p.q.pos, Construct: "delete with no <key> = <parameter> condition", Context: "query " + p.q.Name,
		Hint: "A Q10 delete names the rows it removes by the table's key: <key> = sqlc.arg(<key>), or <key> IN (sqlc.slice(<name>)) (Q7); " + allowedSQL})
}

// claimOps are the comparisons allowed in a Q6 WHERE.
var claimOps = map[string]string{"=": "=", "<>": "<>", "!=": "<>", "<": "<", "<=": "<=", ">": ">", ">=": ">="}

func (p *sqlParser) cond(table string, inGroup bool) (sqlCond, bool) {
	at := p.peek()
	col, ok := p.colRef(table, "a column name in WHERE")
	if !ok {
		return sqlCond{}, false
	}
	if p.is("IN") && p.peekAt(1).up == "(" && p.peekAt(2).up == "SELECT" {
		if inGroup {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: p.peek().pos, Construct: "subquery inside an OR group", Context: "query " + p.q.Name,
				Hint: "A Q9 proof subquery is one condition of its own, joined with AND; " + allowedSQL})
			return sqlCond{}, false
		}
		sub, ok := p.sub(table, col, at)
		if !ok {
			return sqlCond{}, false
		}
		return sqlCond{Col: col, Op: "sub", Sub: sub, pos: at.pos}, true
	}
	if p.is("IN") {
		if inGroup {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: p.peek().pos, Construct: "IN inside an OR group", Context: "query " + p.q.Name,
				Hint: "A Q7 IN list is one condition of its own, joined with AND: <key> IN (sqlc.slice(<name>)); " + allowedSQL})
			return sqlCond{}, false
		}
		v, ok := p.inSlice(col)
		if !ok {
			return sqlCond{}, false
		}
		return sqlCond{Col: col, Op: "in", Val: v, pos: at.pos}, true
	}
	t := p.peek()
	op, ok := claimOps[t.up]
	if t.kind != "punct" || !ok {
		p.fail("a comparison (= <> < <= > >=) after " + col)
		return sqlCond{}, false
	}
	p.i++
	v, ok := p.claimValue(col)
	if !ok {
		return sqlCond{}, false
	}
	return sqlCond{Col: col, Op: op, Val: v, pos: at.pos}, true
}

// colRef reads a column of table in a WHERE, a SELECT list or ORDER BY:
// <col> or <table>.<col>. A column of another table is refused; a plain one
// is remembered, since a statement that names two tables (Q8, Q9) needs the
// table on every column.
func (p *sqlParser) colRef(table, expected string) (string, bool) {
	at := p.peek()
	col, ok := p.ident(expected)
	if !ok {
		return "", false
	}
	if !p.accept(".") {
		p.bare = append(p.bare, at)
		return col, true
	}
	if col != table {
		p.failed = true
		hint := "Each SELECT names one table (no JOIN): write its columns as " + table + ".<col>; " + allowedSQL
		if p.q.JoinTable != "" {
			hint = "Write columns as " + p.q.Table + ".<col> or " + p.q.JoinTable + ".<col>; " + allowedSQL
		}
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "column of table " + col + " where only table " + table + " is in scope", Context: "query " + p.q.Name,
			Hint: hint})
		return "", false
	}
	return p.ident("a column name after " + table + ".")
}

// pageColRef is colRef for a Q5 WHERE/ORDER BY: the column must be of the
// child table (cursor and filters stay on the child when a parent is joined).
func (p *sqlParser) pageColRef(expected string) (string, bool) {
	at := p.peek()
	qual, ok := p.ident(expected)
	if !ok {
		return "", false
	}
	if !p.accept(".") {
		p.bare = append(p.bare, at)
		return qual, true
	}
	tbl, col := qual, ""
	if col, ok = p.ident("a column name after " + tbl + "."); !ok {
		return "", false
	}
	if tbl != p.q.Table {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "column of table " + tbl + " in a keyset page WHERE or ORDER BY", Context: "query " + p.q.Name,
			Hint: "The cursor, equality filters and ORDER BY are on the child table " + p.q.Table + " only; joined parent columns belong in the SELECT list; " + allowedSQL})
		return "", false
	}
	return col, true
}

// checkPageSelect refuses a Q5 SELECT list that names a table other than the
// child (and, with a JOIN, the joined parent), or leaves a column bare when
// two tables are in scope.
func (p *sqlParser) checkPageSelect() {
	if p.failed {
		return
	}
	join := p.q.JoinTable
	for _, sc := range p.selectQual {
		switch {
		case sc.table == "":
			if join != "" {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: sc.at.pos, Construct: "column " + sc.col + " without its table in a keyset page that joins two tables", Context: "query " + p.q.Name,
					Hint: "Write every selected column as " + p.q.Table + ".<col> or " + join + ".<col>; " + allowedSQL})
				return
			}
		case sc.table == p.q.Table:
		case join != "" && sc.table == join:
		default:
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: sc.at.pos, Construct: "column of table " + sc.table + " in a keyset page SELECT", Context: "query " + p.q.Name,
				Hint: "Selected columns are of " + p.q.Table + (map[bool]string{true: " or " + join, false: ""}[join != ""]) + "; " + allowedSQL})
			return
		}
	}
}

// sub reads a Q9 proof subquery after <outer>.<col>: IN (SELECT
// <table>.<key> FROM <table> WHERE <cond> [AND <cond>]...), where <cond> is
// <table>.<col> = <value> or another Q9 subquery. Which table, key and
// conditions A5 accepts is checked against schema.sql (checkProofs).
func (p *sqlParser) sub(outer, outerCol string, at sqlTok) (*sqlSub, bool) {
	const shape = "(SELECT <table>.<key> FROM <table> WHERE ...) after IN"
	if !(p.need("IN", "IN") && p.need("(", shape) && p.need("SELECT", shape)) {
		return nil, false
	}
	sub := &sqlSub{Outer: outer, OuterCol: outerCol, pos: at.pos}
	p.q.Subs = append(p.q.Subs, sub)
	start := p.i
	// The selected column names the subquery's table before FROM does.
	qual, ok := p.ident("<table>.<key>, the one column the subquery selects")
	if !ok || !p.need(".", "<table>.<key>: the subquery's column with its table") {
		return nil, false
	}
	key, ok := p.ident("the key column after " + qual + ".")
	if !ok || !p.need("FROM", "FROM after the subquery's one column") {
		return nil, false
	}
	table, ok := p.ident("a table name")
	if !ok {
		return nil, false
	}
	if qual != table {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: p.toks[start].pos, Construct: "subquery that selects " + qual + "." + key + " FROM " + table, Context: "query " + p.q.Name,
			Hint: "The subquery selects a column of its own table: SELECT " + table + ".<key> FROM " + table + "; " + allowedSQL})
		return nil, false
	}
	sub.Table, sub.Key = table, key
	if !p.need("WHERE", "WHERE after FROM "+table+" (the subquery proves the rows are the signed-in user's)") {
		return nil, false
	}
	for {
		c, ok := p.subCond(table)
		if !ok {
			return nil, false
		}
		sub.Conds = append(sub.Conds, c)
		if !p.accept("AND") {
			break
		}
	}
	if !p.need(")", "AND or ) closing the subquery") {
		return nil, false
	}
	return sub, true
}

// subCond is one condition of a Q9 subquery or a Q8 SELECT: <table>.<col>
// = <value>, or <table>.<col> IN (<Q9 subquery>).
func (p *sqlParser) subCond(table string) (sqlCond, bool) {
	at := p.peek()
	col, ok := p.colRef(table, "<table>.<col> in the subquery's WHERE")
	if !ok {
		return sqlCond{}, false
	}
	if p.is("IN") && p.peekAt(1).up == "(" && p.peekAt(2).up == "SELECT" {
		sub, ok := p.sub(table, col, at)
		if !ok {
			return sqlCond{}, false
		}
		return sqlCond{Col: col, Op: "sub", Sub: sub, pos: at.pos}, true
	}
	if !p.need("=", "= (or IN (SELECT ...)) after "+table+"."+col) {
		return sqlCond{}, false
	}
	v, ok := p.value(col, false)
	if !ok {
		return sqlCond{}, false
	}
	return sqlCond{Col: col, Op: "=", Val: v, pos: at.pos}, true
}

// insertSelect reads the rest of a Q8 insert from a parent row, after
// INSERT INTO <table> (<cols>) SELECT: one item per column, each a value or
// a column of the parent, then FROM <parent> WHERE <cond> [AND <cond>]...
// (conditions as in a Q9 subquery). Which items and conditions A5 accepts is
// checked against schema.sql (checkProofs).
func (p *sqlParser) insertSelect(cols []string) {
	p.q.Shape = "claim"
	type item struct {
		qual, col string
		v         sqlVal
		at        sqlTok
	}
	var items []item
	for {
		at := p.peek()
		if at.kind == "word" && at.up != "SQLC" && !reserved[at.up] || at.kind == "qword" {
			qual, _ := p.ident("a column")
			it := item{at: at}
			if p.accept(".") {
				it.qual = qual
				if it.col, _ = p.ident("a column name after " + qual + "."); p.failed {
					return
				}
			} else {
				it.col = qual
				p.bare = append(p.bare, at)
			}
			items = append(items, it)
		} else {
			if len(items) >= len(cols) {
				break
			}
			v, ok := p.value(cols[len(items)], false)
			if !ok {
				return
			}
			items = append(items, item{v: v, at: at})
		}
		if !p.accept(",") {
			break
		}
	}
	if len(items) != len(cols) {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: p.peek().pos, Construct: fmt.Sprintf("SELECT with %d items for %d columns", len(items), len(cols)),
			Context: "query " + p.q.Name, Hint: "One item per column; " + allowedSQL})
		return
	}
	if !p.need("FROM", "FROM <parent> after the SELECT list") {
		return
	}
	table, ok := p.ident("the parent table")
	if !ok {
		return
	}
	p.q.Source = table
	for i, it := range items {
		v := it.v
		if it.col != "" {
			if it.qual != "" && it.qual != table {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: it.at.pos, Construct: "column of table " + it.qual + " where only table " + table + " is in scope", Context: "query " + p.q.Name,
					Hint: "Write the parent's columns as " + table + ".<col>; " + allowedSQL})
				return
			}
			v = sqlVal{Kind: "col", Lit: it.col}
		}
		p.q.Values = append(p.q.Values, sqlAssign{Col: cols[i], Val: v, pos: it.at.pos})
	}
	if !p.need("WHERE", "WHERE after FROM "+table+" (it names the parent row and proves it is the signed-in user's)") {
		return
	}
	for {
		c, ok := p.subCond(table)
		if !ok {
			return
		}
		p.q.Conds = append(p.q.Conds, c)
		if !p.accept("AND") {
			break
		}
	}
	// Optional RETURNING: the stored child row (:one). Without it the query
	// stays a claim (:execrows, S10 on the changed-row count).
	if !p.accept("RETURNING") {
		return
	}
	p.q.Shape = "insert"
	for {
		col, ok := p.ident("a column name after RETURNING")
		if !ok {
			return
		}
		p.q.Cols = append(p.q.Cols, col)
		if !p.accept(",") {
			return
		}
	}
}

// claimValue is a Q4 value, or (Q6 only) a parameter plus or minus a whole
// number, e.g. sqlc.arg(now) - 600. A bare ? is named after its column by
// sqlc, so a second bare ? for the same column must be named with sqlc.arg.
func (p *sqlParser) claimValue(col string) (sqlVal, bool) {
	if p.failed {
		return sqlVal{}, false
	}
	at := p.peek()
	if at.kind == "param" {
		for _, n := range p.q.Params {
			if n == col {
				p.failed = true
				*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "second bare ? for column " + col, Context: "query " + p.q.Name,
					Hint: "sqlc names a bare ? after its column; name this one with sqlc.arg(<name>)"})
				return sqlVal{}, false
			}
		}
	}
	v, ok := p.value(col, false)
	if !ok {
		return v, false
	}
	return p.offset(v)
}

// offset reads "+ <n>" or "- <n>" after a parameter (Q6, and in Q1/Q2 next
// to the current time): a parameter plus or minus a whole number.
func (p *sqlParser) offset(v sqlVal) (sqlVal, bool) {
	if (p.is("+") || p.is("-")) && v.Kind == "param" {
		v.Op = p.peek().up
		p.i++
		n := p.peek()
		if n.kind != "num" {
			p.fail("a whole number after " + v.Op)
			return sqlVal{}, false
		}
		p.i++
		v.Off = n.text
	}
	return v, true
}

// value reads a parameter, a literal, or (in an insert) the next-number shape.
func (p *sqlParser) value(col string, insert bool) (sqlVal, bool) {
	if p.failed {
		return sqlVal{}, false
	}
	t := p.peek()
	const expected = "a parameter (? or sqlc.arg(name)), an integer or 'text'"
	switch {
	case t.kind == "param":
		p.i++
		p.addParam(col)
		p.uses = append(p.uses, paramUse{name: col, pos: t.pos})
		return sqlVal{Kind: "param", Param: col}, true
	case t.kind == "word" && t.up == "SQLC" && p.peekAt(2).up == "SLICE":
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: t.pos, Construct: "sqlc.slice outside IN", Context: "query " + p.q.Name,
			Hint: "sqlc.slice(<name>) is only the list of a Q7 condition <col> IN (sqlc.slice(<name>)); " + allowedSQL})
		return sqlVal{}, false
	case t.kind == "word" && t.up == "SQLC":
		p.i++
		if !p.need(".", ". after sqlc") || !p.need("ARG", "arg (sqlc.arg)") || !p.need("(", "( after sqlc.arg") {
			return sqlVal{}, false
		}
		// Parameter names may be reserved words (e.g. sqlc.arg(limit)).
		name, ok := p.argName()
		if !ok || !p.need(")", ") after sqlc.arg(name") {
			return sqlVal{}, false
		}
		p.addParam(name)
		p.uses = append(p.uses, paramUse{name: name, pos: t.pos})
		return sqlVal{Kind: "param", Param: name}, true
	case t.kind == "num":
		p.i++
		return sqlVal{Kind: "int", Lit: t.text}, true
	case t.kind == "punct" && t.up == "-" && p.peekAt(1).kind == "num":
		p.i += 2
		return sqlVal{Kind: "int", Lit: "-" + p.toks[p.i-1].text}, true
	case t.kind == "str":
		p.i++
		return sqlVal{Kind: "string", Lit: t.text}, true
	case insert && t.up == "(" && p.peekAt(1).up == "SELECT" && p.peekAt(2).up == "COALESCE":
		return p.nextNumber(col)
	}
	p.fail(expected)
	return sqlVal{}, false
}

// nextNumber: (SELECT COALESCE(MAX(<col>), 0) + 1 FROM <table>) for the same column and table.
func (p *sqlParser) nextNumber(col string) (sqlVal, bool) {
	const shape = "the next-number shape (SELECT COALESCE(MAX(<col>), 0) + 1 FROM <table>)"
	if !(p.need("(", shape) && p.need("SELECT", shape) && p.need("COALESCE", shape) && p.need("(", shape) &&
		p.need("MAX", shape) && p.need("(", shape)) {
		return sqlVal{}, false
	}
	at := p.peek()
	c, ok := p.ident("the column " + col)
	if !ok {
		return sqlVal{}, false
	}
	if !(p.need(")", shape) && p.need(",", shape) && p.need("0", shape) && p.need(")", shape) && p.need("+", shape) &&
		p.need("1", shape) && p.need("FROM", shape)) {
		return sqlVal{}, false
	}
	at2 := p.peek()
	tbl, ok := p.ident("the table " + p.q.Table)
	if !ok || !p.need(")", ") closing "+shape) {
		return sqlVal{}, false
	}
	for _, chk := range []struct {
		got, want string
		at        sqlTok
	}{{c, col, at}, {tbl, p.q.Table, at2}} {
		if chk.got != chk.want {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: chk.at.pos, Construct: "next-number subquery over " + chk.got, Context: "query " + p.q.Name,
				Hint: "The next number of `" + col + "` must come from MAX(" + col + ") of the same table " + p.q.Table + "; " + allowedSQL})
			return sqlVal{}, false
		}
	}
	return sqlVal{Kind: "next"}, true
}

// Q7: <col> IN (sqlc.slice(<name>)), at most once per query. The list is
// bound to a D10 list input by the action (S3).
func (p *sqlParser) inSlice(col string) (sqlVal, bool) {
	at := p.peek()
	if !p.need("IN", "IN") {
		return sqlVal{}, false
	}
	const shape = "(sqlc.slice(<name>)) after IN"
	if !p.need("(", shape) {
		return sqlVal{}, false
	}
	if !p.is("SQLC") || p.peekAt(2).up != "SLICE" {
		p.fail(shape + " (a Q7 IN list is one sqlc.slice parameter, never a list of values or a subquery)")
		return sqlVal{}, false
	}
	start := p.peek()
	if !(p.need("SQLC", shape) && p.need(".", shape) && p.need("SLICE", shape) && p.need("(", shape)) {
		return sqlVal{}, false
	}
	name, ok := p.argName()
	if !ok || !p.need(")", ") after sqlc.slice(name") || !p.need(")", ") closing IN (sqlc.slice(name))") {
		return sqlVal{}, false
	}
	if p.q.Slice != "" {
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: at.pos, Construct: "second IN (sqlc.slice(...))", Context: "query " + p.q.Name,
			Hint: "A query has at most one Q7 IN list; " + allowedSQL})
		return sqlVal{}, false
	}
	for _, n := range p.q.Params {
		if n == name {
			p.failed = true
			*p.errs = append(*p.errs, Refusal{Pos: start.pos, Construct: "sqlc.slice(" + name + ") named like another parameter", Context: "query " + p.q.Name,
				Hint: "Give the list its own name"})
			return sqlVal{}, false
		}
	}
	p.q.Slice, p.q.SliceCol, p.q.slicePos = name, col, at.pos
	p.addParam(name)
	p.uses = append(p.uses, paramUse{name: name, slice: true, pos: start.pos})
	return sqlVal{Kind: "slice", Param: name}, true
}

// sliceLast refuses a parameter after the Q7 list. sqlc numbers the
// parameters of a statement as if sqlc.slice(<name>) were one value and
// expands it into one ? per entry when the query runs; SQLite gives each of
// those ? the next number, so a numbered parameter after the slice (?3)
// would be bound to one of the list's entries instead of its own value.
// Before the slice every parameter keeps its number.
func (p *sqlParser) sliceLast() {
	for i, u := range p.uses {
		if !u.slice || i == len(p.uses)-1 {
			continue
		}
		next := p.uses[i+1]
		p.failed = true
		*p.errs = append(*p.errs, Refusal{Pos: next.pos, Construct: "parameter " + next.name + " after IN (sqlc.slice(" + u.name + "))", Context: "query " + p.q.Name,
			Hint: "sqlc numbers the parameters as if the slice were one value, so with SQLite a parameter after it is bound to one of the list's entries instead of its own value; put " + p.q.SliceCol + " IN (sqlc.slice(" + u.name + ")) last in the statement"})
		return
	}
}

func tables(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	if len(names) == 1 {
		return "table " + quoted[0]
	}
	return "tables " + joinList(quoted)
}

// checkPageJoins refuses a Q5 JOIN whose ON is not child.fk = parent.pk
// against schema.sql's single-column PRIMARY KEY of the parent.
func checkPageJoins(keys map[string]string, queries map[string]*SQLQuery) Refusals {
	var errs Refusals
	for _, name := range sortedKeys(queries) {
		q := queries[name]
		if q.bad || q.JoinTable == "" {
			continue
		}
		pk := keys[q.JoinTable]
		if pk == "" {
			errs = append(errs, Refusal{Pos: q.pos, Construct: fmt.Sprintf("JOIN of table %s (query %s), which schema.sql does not give a single-column PRIMARY KEY", q.JoinTable, q.Name),
				Context: "Q5 keyset page", Hint: "The equijoin is <child>.<fk> = <parent>.<pk> where <pk> is the parent's single-column PRIMARY KEY; " + allowedSQL})
			q.bad = true
			continue
		}
		if q.JoinPK != pk {
			errs = append(errs, Refusal{Pos: q.pos, Construct: fmt.Sprintf("JOIN ON %s.%s = %s.%s (query %s), but %s's primary key is %s", q.Table, q.JoinFK, q.JoinTable, q.JoinPK, q.Name, q.JoinTable, pk),
				Context: "Q5 keyset page", Hint: "Write JOIN " + q.JoinTable + " ON " + q.Table + ".<fk> = " + q.JoinTable + "." + pk + "; " + allowedSQL})
			q.bad = true
		}
	}
	return errs
}

// plural and singular turn a table name into words: line_items -> "line items" / "line item".
func plural(table string) string { return strings.ReplaceAll(table, "_", " ") }

func singular(table string) string {
	s := plural(table)
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	}
	return s
}

// norm compares SQL and Go names: customer_id ~ CustomerID.
func norm(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }

// sqlValText is a value as written in SQL, for refusals.
func sqlValText(v sqlVal) string {
	if v.Kind == "param" {
		return sqlParamText(v)
	}
	return v.Lit
}
