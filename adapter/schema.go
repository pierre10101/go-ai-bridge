package adapter

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
