package adapter

import (
	"errors"
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Q10 delete. DELETE FROM <table> WHERE <cond> [AND <cond>]... is a claim
// with no SET: the statement that deletes checks its conditions, it names
// its rows by the table's single-column PRIMARY KEY (<key> = <parameter>, or
// a Q7 <key> IN (sqlc.slice(<name>))), and S10 (or S11) checks how many rows
// it removed. A4 and A5 apply to it as to a Q6 update: an action that a role
// without the ownership bypass may call deletes an owned table's rows only
// with <owner col> = <the signed-in user> in its WHERE, and a child table's
// only with the Q9 proof.
//
// What happens to the rows that reference a deleted row (grandchildren:
// the seats of a deleted section) is decided by schema.sql, never silently:
// every foreign key that references the table (and, through ON DELETE
// CASCADE, every table deleted with it) declares ON DELETE CASCADE (the rows
// are deleted with it, said in the English) or ON DELETE RESTRICT (the
// delete fails while such a row exists: the query fails and nothing is
// deleted). Any other foreign key (no ON DELETE, NO ACTION, SET NULL, SET
// DEFAULT) is refused at -check: SQLite would fail the delete with no word
// in the English, or change rows the English does not mention. store.Open
// turns foreign keys on (PRAGMA foreign_keys), so the database does what
// the English says.

// fkRef is one foreign key of schema.sql: Table.Col REFERENCES
// RefTable(RefCol), with its ON DELETE action ("CASCADE", "RESTRICT", "SET
// NULL", "SET DEFAULT", "NO ACTION", or "" when it declares none).
type fkRef struct {
	Table, Col, RefTable, RefCol, OnDelete string
	pos                                    token.Position
}

// fkEffect is what a Q10 delete does to the rows of Table whose Col holds
// the RefCol of a deleted row of RefTable: deleted with it (CASCADE) or the
// delete fails while one exists (RESTRICT).
type fkEffect struct {
	Table, Col, RefTable, RefCol, OnDelete string
}

// foreignKeys reads every foreign key of <root>/schema.sql: a column's
// REFERENCES <table> [(<col>)] [ON DELETE <action>], or a table constraint
// [CONSTRAINT <name>] FOREIGN KEY (<col>, ...) REFERENCES <table> [(<col>,
// ...)] [ON DELETE <action>]. A reference without a column list names the
// referenced table's primary key.
func foreignKeys(root string, keys map[string]string) ([]fkRef, error) {
	path := filepath.Join(root, "schema.sql")
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var toks []sqlTok
	for n, line := range strings.Split(string(src), "\n") {
		toks = append(toks, tokenizeSQL(line, "schema.sql", n+1)...)
	}
	var out []fkRef
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
		for _, it := range items {
			if len(it) > 2 && it[0].up == "CONSTRAINT" {
				it = it[2:]
			}
			if len(it) == 0 {
				continue
			}
			var cols []string
			k := 0
			switch it[0].up {
			case "FOREIGN":
				for k = 1; k < len(it) && it[k].up != "REFERENCES"; k++ {
					if w := it[k]; (w.kind == "word" || w.kind == "qword") && w.up != "KEY" {
						cols = append(cols, strings.ToLower(w.text))
					}
				}
			case "PRIMARY", "UNIQUE", "CHECK":
				continue
			default:
				cols = []string{strings.ToLower(it[0].text)}
				for k = 1; k < len(it) && it[k].up != "REFERENCES"; k++ {
				}
			}
			if k+1 >= len(it) {
				continue
			}
			ref := fkRef{Table: table, Col: strings.Join(cols, ", "), RefTable: strings.ToLower(it[k+1].text), pos: it[k].pos}
			k += 2
			var refCols []string
			if k < len(it) && it[k].up == "(" {
				for k++; k < len(it) && it[k].up != ")"; k++ {
					if it[k].up != "," {
						refCols = append(refCols, strings.ToLower(it[k].text))
					}
				}
			}
			ref.RefCol = strings.Join(refCols, ", ")
			if ref.RefCol == "" {
				ref.RefCol = keys[ref.RefTable]
			}
			for ; k+2 < len(it); k++ {
				if it[k].up == "ON" && it[k+1].up == "DELETE" {
					ref.OnDelete = it[k+2].up
					if k+3 < len(it) && (it[k+2].up == "SET" || it[k+2].up == "NO") {
						ref.OnDelete += " " + it[k+3].up
					}
				}
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

// checkDeletes refuses, against schema.sql, every Q10 delete that does not
// name its rows by its table's single-column PRIMARY KEY, or whose rows (or
// rows deleted with them by ON DELETE CASCADE) a foreign key references
// without ON DELETE CASCADE or ON DELETE RESTRICT. For an accepted delete it
// records those effects (q.OnDelete, for the English) and adds every table
// a cascade deletes from to q.Writes.
func checkDeletes(root string, queries map[string]*SQLQuery) (Refusals, error) {
	var errs Refusals
	var keys map[string]string
	var fks []fkRef
	for _, name := range sortedKeys(queries) {
		q := queries[name]
		if q.bad || !q.Delete {
			continue
		}
		if keys == nil {
			var err error
			if keys, err = primaryKeys(root); err != nil {
				return nil, err
			}
			if fks, err = foreignKeys(root, keys); err != nil {
				return nil, err
			}
		}
		refuse := func(construct, hint string) {
			errs = append(errs, Refusal{Pos: q.pos, Construct: construct, Context: "Q10 delete", Hint: hint})
			q.bad = true
		}
		key := keys[q.Table]
		if key == "" {
			refuse(fmt.Sprintf("delete from table %s (query %s), which schema.sql does not give a single-column PRIMARY KEY", q.Table, q.Name),
				"A Q10 delete names the rows it removes by the table's key, so each value is at most one row; declare <col> INTEGER PRIMARY KEY (or PRIMARY KEY (<col>)) for "+q.Table+" in schema.sql")
			continue
		}
		byKey := false
		for _, c := range q.Conds {
			if c.Col == key && (c.Op == "=" && c.Val.Kind == "param" && c.Val.Op == "" || c.Op == "in") {
				byKey = true
			}
		}
		if !byKey {
			refuse(fmt.Sprintf("delete from table %s (query %s) whose WHERE does not name its rows by the key %s", q.Table, q.Name, key),
				fmt.Sprintf("A Q10 delete removes rows by their key, never by another column (which could match any number of rows): DELETE FROM %s WHERE %s = sqlc.arg(%s) [AND <col> = <value>]... (or %s IN (sqlc.slice(<name>)), Q7), then if <n> != 1 { return Output{}, F<n> } (S10)", q.Table, key, key, key))
			continue
		}
		var effects []fkEffect
		seen := map[string]bool{q.Table: true}
		queue := []string{q.Table}
		for len(queue) > 0 && !q.bad {
			t := queue[0]
			queue = queue[1:]
			for _, fk := range fks {
				if fk.RefTable != t {
					continue
				}
				switch fk.OnDelete {
				case "CASCADE":
					effects = append(effects, fkEffect{fk.Table, fk.Col, fk.RefTable, fk.RefCol, fk.OnDelete})
					q.Writes = appendUnique(q.Writes, fk.Table)
					if !seen[fk.Table] {
						seen[fk.Table] = true
						queue = append(queue, fk.Table)
					}
				case "RESTRICT":
					effects = append(effects, fkEffect{fk.Table, fk.Col, fk.RefTable, fk.RefCol, fk.OnDelete})
				default:
					how := "without ON DELETE"
					if fk.OnDelete != "" {
						how = "with ON DELETE " + fk.OnDelete
					}
					through := ""
					if t != q.Table { // reached through ON DELETE CASCADE
						through = fmt.Sprintf(", which deletes rows of %s with it (ON DELETE CASCADE),", t)
					}
					refuse(fmt.Sprintf("delete from table %s (query %s)%s while %s.%s references %s %s (%s)", q.Table, q.Name, through, fk.Table, fk.Col, t, how, fk.pos),
						fmt.Sprintf("A delete says what happens to the rows that point at a deleted row: declare ON DELETE CASCADE (they are deleted with it, and the English says so) or ON DELETE RESTRICT (the delete fails while one exists: the query fails and nothing is deleted) on %s REFERENCES %s (%s) in schema.sql. Without either SQLite fails the delete (NO ACTION) with no word in the English, and SET NULL or SET DEFAULT would change rows the English does not mention", fk.Col, t, fk.RefCol))
				}
				if q.bad {
					break
				}
			}
		}
		if !q.bad {
			q.OnDelete = effects
		}
	}
	return errs, nil
}
