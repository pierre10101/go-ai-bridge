package adapter

import (
	"bufio"
	"errors"
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// primaryKeys reads <root>/schema.sql, the app's schema (see App layout),
// and returns each table's single-column PRIMARY KEY: a column defined with
// PRIMARY KEY, or a table constraint PRIMARY KEY (<col>) with one column. A
// table with a composite key, or none, is not in the map. A missing
// schema.sql gives an empty map. Q7 uses it: a claim over IN
// (sqlc.slice(...)) must name its rows by the key, so each entry of the list
// is at most one row and the S11 check can compare the count with the list.
func primaryKeys(root string) (map[string]string, error) {
	path := filepath.Join(root, "schema.sql")
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var toks []sqlTok
	sc := bufio.NewScanner(fh)
	for n := 1; sc.Scan(); n++ {
		toks = append(toks, tokenizeSQL(sc.Text(), path, n)...)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	keys := map[string]string{}
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].up != "CREATE" || toks[i+1].up != "TABLE" {
			continue
		}
		j := i + 2
		if j+2 < len(toks) && toks[j].up == "IF" && toks[j+1].up == "NOT" && toks[j+2].up == "EXISTS" {
			j += 3
		}
		if j+1 >= len(toks) || toks[j+1].up != "(" {
			continue
		}
		table := strings.ToLower(toks[j].text)
		items, end := tableItems(toks, j+2)
		i = end
		var found []string
		for _, it := range items {
			if len(it) == 0 {
				continue
			}
			switch it[0].up {
			case "PRIMARY":
				// PRIMARY KEY (<col>[, <col>]...)
				if len(it) >= 5 && it[1].up == "KEY" && it[2].up == "(" {
					var cols []string
					for _, t := range it[3:] {
						if t.up == ")" {
							break
						}
						if t.up != "," {
							cols = append(cols, strings.ToLower(t.text))
						}
					}
					found = append(found, strings.Join(cols, ","))
				}
			case "CONSTRAINT", "UNIQUE", "CHECK", "FOREIGN":
			default:
				for k := 1; k+1 < len(it); k++ {
					if it[k].up == "PRIMARY" && it[k+1].up == "KEY" {
						found = append(found, strings.ToLower(it[0].text))
					}
				}
			}
		}
		if len(found) == 1 && !strings.Contains(found[0], ",") {
			keys[table] = found[0]
		}
	}
	return keys, nil
}

// tableItems splits the body of CREATE TABLE <name> ( ... ) at its top-level
// commas, from toks[start] to the closing parenthesis; end is its index.
func tableItems(toks []sqlTok, start int) ([][]sqlTok, int) {
	var items [][]sqlTok
	var cur []sqlTok
	depth := 0
	for i := start; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.up == "(":
			depth++
		case t.up == ")" && depth == 0:
			return append(items, cur), i
		case t.up == ")":
			depth--
		case t.up == "," && depth == 0:
			items = append(items, cur)
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	return append(items, cur), len(toks)
}

// checkClaimKeys refuses a Q6 claim whose Q7 IN list is not on its table's
// single-column PRIMARY KEY (schema.sql).
func checkClaimKeys(root string, queries map[string]*SQLQuery) (Refusals, error) {
	var errs Refusals
	var keys map[string]string
	for _, name := range sortedKeys(queries) {
		q := queries[name]
		if q.bad || q.Shape != "claim" || q.Slice == "" {
			continue
		}
		if keys == nil {
			var err error
			if keys, err = primaryKeys(root); err != nil {
				return nil, err
			}
		}
		if keys[q.Table] != q.SliceCol {
			errs = append(errs, Refusal{Pos: q.slicePos, Construct: "claim over IN (sqlc.slice(" + q.Slice + ")) on column " + q.SliceCol +
				", which schema.sql does not declare as the single-column PRIMARY KEY of table " + q.Table, Context: "query " + q.Name,
				Hint: "A multi-row claim names its rows by the table's key, so each entry of the list is at most one row and S11 can compare the number of rows changed with the number of entries: <key> IN (sqlc.slice(<name>))"})
			q.bad = true
		}
	}
	return errs, nil
}

// owner is one table's A4 ownership: schema.sql declares its owner column
// with the comment `-- owner: <col>` attached to its CREATE TABLE (on the
// comment lines right above it, with no blank line in between, or at the
// end of the CREATE TABLE line itself).
type owner struct {
	Table, Col string
	GoType     string // the user field type the column takes: int64 (INTEGER) or string (TEXT)
	pos        token.Position
}

var (
	ownerLineRe = regexp.MustCompile(`(?i)^owner\s*:`)
	ownerRe     = regexp.MustCompile(`^owner: ([a-z_][a-z0-9_]*)$`)
)

// ownerHint is attached to every refusal of an owner annotation.
const ownerHint = "Declare a table's owner once, on a comment line right above its CREATE TABLE (no blank line in between): -- owner: <col>, where <col> is one of its columns of type INTEGER (an int64 signed-in user, server:\"user\") or TEXT (a string one)"

// loadOwners reads A4: every `-- owner: <col>` annotation of
// <root>/schema.sql, keyed by table. A missing schema.sql gives an empty
// map. A malformed annotation, one attached to no CREATE TABLE, a second one
// for a table, a column the table does not declare, or a column that is
// neither INTEGER nor TEXT is refused (with the position schema.sql:L:C) and
// that table is not owned.
func loadOwners(root string) (map[string]*owner, Refusals, error) {
	path := filepath.Join(root, "schema.sql")
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]*owner{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	const name = "schema.sql"
	var errs Refusals
	refuse := func(pos token.Position, construct string) {
		errs = append(errs, Refusal{Pos: pos, Construct: construct, Context: "A4 ownership", Hint: ownerHint})
	}
	type anno struct {
		col string
		pos token.Position
	}
	var pending []anno // annotations of the comment block being read
	var toks []sqlTok
	attached := map[int][]anno{} // index of the CREATE token -> its annotations
	flushDangling := func() {
		for _, a := range pending {
			refuse(a.pos, "owner annotation \"-- owner: "+a.col+"\" that is not attached to a CREATE TABLE")
		}
		pending = nil
	}
	lines := strings.Split(string(src), "\n")
	for n, raw := range lines {
		line := strings.TrimSpace(raw)
		lineToks := tokenizeSQL(raw, name, n+1)
		var comment string
		var col int
		if i := sqlCommentStart(raw); i >= 0 {
			comment, col = strings.TrimSpace(raw[i+2:]), i+1
		}
		var here *anno
		if comment != "" && ownerLineRe.MatchString(comment) {
			pos := token.Position{Filename: name, Line: n + 1, Column: col}
			if m := ownerRe.FindStringSubmatch(comment); m != nil {
				here = &anno{col: m[1], pos: pos}
			} else {
				refuse(pos, "owner annotation "+strconv.Quote("-- "+comment))
			}
		}
		switch {
		case line == "":
			flushDangling()
		case len(lineToks) == 0: // a comment line
			if here != nil {
				pending = append(pending, *here)
			}
		default:
			isCreate := len(lineToks) >= 2 && lineToks[0].up == "CREATE" && lineToks[1].up == "TABLE"
			if here != nil {
				pending = append(pending, *here)
			}
			if isCreate {
				attached[len(toks)] = pending
				pending = nil
			} else {
				flushDangling()
			}
			toks = append(toks, lineToks...)
		}
	}
	flushDangling()
	owners := map[string]*owner{}
	for i := 0; i+2 < len(toks); i++ {
		annos, ok := attached[i]
		if !ok || toks[i].up != "CREATE" || toks[i+1].up != "TABLE" {
			continue
		}
		j := i + 2
		if j+2 < len(toks) && toks[j].up == "IF" && toks[j+1].up == "NOT" && toks[j+2].up == "EXISTS" {
			j += 3
		}
		if j+1 >= len(toks) || toks[j+1].up != "(" {
			continue
		}
		table := strings.ToLower(toks[j].text)
		items, _ := tableItems(toks, j+2)
		cols := map[string]string{} // column -> its declared type, upper case ("" if none)
		var names []string
		for _, it := range items {
			if len(it) == 0 {
				continue
			}
			switch it[0].up {
			case "PRIMARY", "CONSTRAINT", "UNIQUE", "CHECK", "FOREIGN":
				continue
			}
			typ := ""
			if len(it) > 1 && (it[1].kind == "word") {
				typ = it[1].up
			}
			cols[strings.ToLower(it[0].text)] = typ
			names = append(names, strings.ToLower(it[0].text))
		}
		for k, a := range annos {
			if k > 0 {
				refuse(a.pos, fmt.Sprintf("second owner annotation for table %s (the first is at %s)", table, annos[0].pos))
				continue
			}
			typ, ok := cols[a.col]
			switch {
			case !ok:
				refuse(a.pos, fmt.Sprintf("owner column %s, which table %s does not declare (its columns: %s)", a.col, table, strings.Join(names, ", ")))
			case typ != "INTEGER" && typ != "TEXT":
				refuse(a.pos, fmt.Sprintf("owner column %s of type %q, which is neither INTEGER nor TEXT", a.col, typ))
			default:
				owners[table] = &owner{Table: table, Col: a.col, GoType: map[string]string{"INTEGER": "int64", "TEXT": "string"}[typ], pos: a.pos}
			}
		}
	}
	return owners, errs, nil
}

// sqlCommentStart is the index of the -- that starts a comment in line
// (outside quoted text and identifiers), or -1: where tokenizeSQL stops.
func sqlCommentStart(line string) int {
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\'' || c == '"' || c == '`':
			for i++; i < len(line) && line[i] != c; i++ {
			}
		case c == '-' && i+1 < len(line) && line[i+1] == '-':
			return i
		}
	}
	return -1
}
