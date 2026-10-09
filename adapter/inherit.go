package adapter

import (
	"fmt"
	"go/ast"
	"strings"
)

// A5 inherited ownership. A child table names the owned table its rows
// belong to (`-- owner: event_id -> events.organizer_id`, loadOwners); the
// parent may be a child itself, so ownership runs up a chain to a table
// owned directly (A4). Copying the owner column onto the child row proves
// nothing, so every write to a child table from an action that a role
// without the ownership bypass may call proves, in the statement that
// writes, that the parent row is the signed-in user's:
//
//   - Q8, an insert from the parent row: INSERT INTO <child> (<col>, ...)
//     SELECT <parent>.<key>, <value>, ... FROM <parent> WHERE <parent>.<key>
//     = <value> AND <proof of parent>. It adds one row or none (:execrows,
//     S10), so another user's parent adds nothing.
//   - Q9, a proof subquery, as an AND condition of a Q6 claim's WHERE:
//     <child>.<col> IN (SELECT <parent>.<key> FROM <parent> WHERE <proof of
//     parent>).
//
// <proof of parent> is <parent>.<owner col> = <the server:"user" field> when
// the parent is owned directly, or the parent's own Q9 subquery when it is a
// child too. A claim never changes the child's parent column. Reads are not
// refused; the English says whether a read is limited to the caller's rows.

// checkProofs refuses, against schema.sql, every Q8 insert from a parent row
// and Q9 subquery that is not exactly the ownership proof of its table (A5):
// the table, key, columns and conditions the annotations name. Whether the
// value compared with the owner column is the signed-in user is checked
// where the query is called (childOwnership).
func checkProofs(owners map[string]*owner, queries map[string]*SQLQuery) Refusals {
	var errs Refusals
	for _, name := range sortedKeys(queries) {
		q := queries[name]
		if q.bad || (len(q.Subs) == 0 && q.Source == "") {
			continue
		}
		refuse := func(c sqlCond, construct, hint string) {
			pos := c.pos
			if pos.Line == 0 {
				pos = q.pos
			}
			errs = append(errs, Refusal{Pos: pos, Construct: construct, Context: "A5 inherited ownership", Hint: hint})
			q.bad = true
		}
		var top []sqlCond
		if q.Shape == "claim" && q.Source == "" {
			top = q.Conds
		}
		for _, c := range q.Where {
			top = append(top, sqlCond{Col: c.Col, Op: c.Op, Val: c.Val, Sub: c.Sub, pos: c.pos})
		}
		for _, c := range top {
			if c.Op != "sub" {
				continue
			}
			if construct, hint := badProof(owners, q.Table, c); construct != "" {
				refuse(c, construct, hint)
			}
		}
		if q.Source == "" || q.bad {
			continue
		}
		o := owners[q.Table]
		at := sqlCond{pos: q.pos}
		switch {
		case o == nil || o.Parent == nil:
			refuse(at, fmt.Sprintf("insert from a row of %s into table %s, which does not inherit its owner", q.Source, q.Table),
				"A Q8 insert from a parent row (INSERT ... SELECT) adds a row to a table that inherits its owner (A5, -- owner: <col> -> <parent>.<pcol> in schema.sql) from that parent; any other insert is a Q3 INSERT ... VALUES")
			continue
		case o.Parent.Table != q.Source:
			refuse(at, fmt.Sprintf("insert into table %s from a row of %s, but %s inherits its owner from %s", q.Table, q.Source, q.Table, o.Parent.Table), "Write it as "+q8SQL(o))
			continue
		}
		for _, v := range q.Values {
			switch {
			case v.Col == o.Col && (v.Val.Kind != "col" || v.Val.Lit != o.ParentKey):
				refuse(sqlCond{pos: v.pos}, fmt.Sprintf("value of the parent column %s that is not %s.%s", o.Col, q.Source, o.ParentKey), "The new row's parent is the row the SELECT proves: "+q8SQL(o))
			case v.Col != o.Col && v.Val.Kind == "col":
				refuse(sqlCond{pos: v.pos}, fmt.Sprintf("copy of %s.%s into %s", q.Source, v.Val.Lit, v.Col), "Only the parent column takes a column of the parent (its key); every other value is a parameter or a literal: "+q8SQL(o))
			}
		}
		key, proof := false, false
		for _, c := range q.Conds {
			switch {
			case c.Op == "=" && c.Col == o.ParentKey && c.Val.Kind == "param" && !key:
				key = true
			case !proof && badProofCond(owners, o.Parent, c) == "":
				proof = true
			default:
				refuse(c, fmt.Sprintf("condition on %s.%s in an insert from a parent row", q.Source, c.Col), "The WHERE names the parent row by its key and proves it is the signed-in user's, nothing else: "+q8SQL(o))
			}
		}
		if !q.bad && (!key || !proof) {
			refuse(at, fmt.Sprintf("insert from a row of %s whose WHERE does not name it by %s.%s = <parameter> and prove it is the signed-in user's", q.Source, q.Source, o.ParentKey), "Write it as "+q8SQL(o))
		}
	}
	return errs
}

// badProof checks one Q9 subquery compared with column c.Col of table: it is
// table's ownership proof, or the returned construct says how it is not.
func badProof(owners map[string]*owner, table string, c sqlCond) (construct, hint string) {
	o := owners[table]
	if o == nil || o.Parent == nil {
		return fmt.Sprintf("subquery on %s.%s, but table %s does not inherit its owner", table, c.Col, table),
			"A Q9 subquery is only the proof that a child row's parent is the signed-in user's (A5: -- owner: <col> -> <parent>.<pcol> on the child's CREATE TABLE in schema.sql); " + allowedSQL
	}
	if reason := badProofCond(owners, o, c); reason != "" {
		return reason, "Table " + table + " inherits its owner from " + o.Parent.Table + " (" + o.pos.String() + "); its proof is exactly " + proofSQL(o)
	}
	return "", ""
}

// badProofCond reports how condition c, on the rows of o.Table, is not the
// proof that they are the signed-in user's ("" when it is): o's owner column
// = <value> when o is owned directly, otherwise o's parent column IN (the
// parent's proof subquery).
func badProofCond(owners map[string]*owner, o *owner, c sqlCond) string {
	if o.Parent == nil {
		if c.Op == "=" && c.Col == o.Col {
			return ""
		}
		return fmt.Sprintf("condition on %s.%s where the proof compares %s.%s with the signed-in user", o.Table, c.Col, o.Table, o.Col)
	}
	sub := c.Sub
	switch {
	case c.Op != "sub" || c.Col != o.Col:
		return fmt.Sprintf("condition on %s.%s where the proof is %s.%s IN (SELECT %s.%s FROM %s ...)", o.Table, c.Col, o.Table, o.Col, o.Parent.Table, o.ParentKey, o.Parent.Table)
	case sub.Table != o.Parent.Table || sub.Key != o.ParentKey:
		return fmt.Sprintf("subquery %s.%s IN (SELECT %s.%s FROM %s ...), but %s.%s holds the key %s.%s", o.Table, c.Col, sub.Table, sub.Key, sub.Table, o.Table, o.Col, o.Parent.Table, o.ParentKey)
	case len(sub.Conds) != 1:
		return fmt.Sprintf("subquery on %s with %d conditions", sub.Table, len(sub.Conds))
	}
	if r := badProofCond(owners, o.Parent, sub.Conds[0]); r != "" {
		return r
	}
	return ""
}

// proofSQL is o's ownership proof written as SQL, for hints: the condition
// on o's own rows (a nested Q9 subquery for each inherited level).
func proofSQL(o *owner) string {
	if o.Parent == nil {
		return o.Table + "." + o.Col + " = sqlc.arg(" + o.Col + ")"
	}
	return o.Table + "." + o.Col + " IN (SELECT " + o.Parent.Table + "." + o.ParentKey + " FROM " + o.Parent.Table + " WHERE " + proofSQL(o.Parent) + ")"
}

// q8SQL is the Q8 insert from a parent row into child table o, for hints.
func q8SQL(o *owner) string {
	p := o.Parent
	return "INSERT INTO " + o.Table + " (" + o.Col + ", <col>, ...) SELECT " + p.Table + "." + o.ParentKey + ", <value>, ... FROM " + p.Table +
		" WHERE " + p.Table + "." + o.ParentKey + " = sqlc.arg(" + o.Col + ") AND " + proofSQL(p) + " (:execrows, then if <n> != 1 { return Output{}, F<n> }, S10)"
}

// chainRows is the plural phrase of o's rows through the chain: "sections
// of events"; chainRow the singular one with its article: "a section of an
// event". rootCol is the owner column at the top: "events.organizer_id".
func chainRows(o *owner) string {
	if o.Parent == nil {
		return plural(o.Table)
	}
	return fmt.Sprintf(t("of"), plural(o.Table), chainRows(o.Parent))
}

func chainRow(o *owner) string {
	if o.Parent == nil {
		return article(singular(o.Table))
	}
	return fmt.Sprintf(t("of"), article(singular(o.Table)), chainRow(o.Parent))
}

func rootCol(o *owner) string {
	r := o.root()
	return r.Table + "." + r.Col
}

// childOwnership applies A5 to query q on child table o.Table, called at
// statement s, and returns the sentence its step says about ownership.
func (w *walker) childOwnership(s ast.Stmt, q *SQLQuery, o *owner) string {
	rows, col := chainRows(o), rootCol(o)
	var proven bool
	var other string
	var otherAt ast.Node
	switch {
	case q.Source != "": // Q8: the WHERE is on the parent's rows
		proven, other, otherAt = w.proven(q.Conds, o.Parent)
	case q.Shape == "claim":
		proven, other, otherAt = w.proven(q.Conds, o)
	default:
		var conds []sqlCond
		for _, c := range q.Where {
			conds = append(conds, sqlCond{Col: c.Col, Op: c.Op, Val: c.Val, Sub: c.Sub})
		}
		proven, other, otherAt = w.proven(conds, o)
	}
	if len(q.Writes) == 0 {
		if proven {
			return fmt.Sprintf(t("owned read"), rows, col)
		}
		return fmt.Sprintf(t("unowned read"), rows, col)
	}
	childWrite := func() string {
		if q.Source != "" {
			return fmt.Sprintf(t("child insert"), singular(q.Table), chainRow(o.Parent), col, singular(o.Parent.Table))
		}
		return fmt.Sprintf(t("owned update"), rows, col)
	}
	if bypass := w.bypassRoles(); bypass != nil {
		if proven {
			return childWrite()
		}
		key := "bypass write"
		if len(bypass) > 1 {
			key = "bypass write n"
		}
		return fmt.Sprintf(t(key), rows, col, joinOr(bypass))
	}
	r := o.root()
	shape := proofSQL(o)
	if q.Shape == "insert" || q.Source != "" {
		shape = q8SQL(o)
	}
	hint := strings.NewReplacer("{table}", q.Table, "{parent}", o.Parent.Table, "{col}", o.Col, "{at}", o.pos.String(), "{shape}", shape,
		"{owner}", r.Col, "{type}", r.GoType, "{param}", sqlcField(r.Col)).Replace(childWriteHint)
	refuse := func(n ast.Node, construct string) {
		if n == nil {
			n = s
		}
		w.refuse(n, construct, "A5 inherited ownership", hint)
	}
	from := fmt.Sprintf("table %s (query %s), which inherits its owner from %s,", q.Table, q.Name, o.Parent.Table)
	switch {
	case w.f.Public:
		refuse(s, "write to "+from+" in a Public action")
	case w.userField() == nil:
		refuse(s, "write to "+from+" in an action without the signed-in user")
	case other != "":
		refuse(otherAt, fmt.Sprintf("write to %s whose proof compares %s with %s, which is not the signed-in user", from, col, other))
	case !proven && w.copiesOwner(q, r.Col):
		refuse(s, fmt.Sprintf("write to table %s (query %s) that proves ownership with its own copy of %s, which proves nothing: %s inherits its owner from %s through %s", q.Table, q.Name, r.Col, q.Table, o.Parent.Table, o.Col))
	case !proven && q.Shape == "insert":
		refuse(s, fmt.Sprintf("insert into %s that does not prove the %s is the signed-in user's", from, singular(o.Parent.Table)))
	case !proven:
		refuse(s, fmt.Sprintf("write to %s whose WHERE does not prove the %s is the signed-in user's", from, singular(o.Parent.Table)))
	}
	if q.Shape == "claim" && q.Source == "" {
		for _, v := range q.Values {
			if v.Col == o.Col {
				refuse(s, fmt.Sprintf("change of the parent column %s of table %s (query %s), which inherits its owner from %s", o.Col, q.Table, q.Name, o.Parent.Table))
			}
		}
	}
	return childWrite()
}

// proven reports whether conds, on the rows of o.Table, contain o's
// ownership proof (as an AND condition) ending in the owner column compared
// with exactly the server:"user" field; other is the English of the value
// compared instead, at otherAt.
func (w *walker) proven(conds []sqlCond, o *owner) (ok bool, other string, otherAt ast.Node) {
	for _, c := range conds {
		if c.Any != nil || c.Col != o.Col {
			continue
		}
		if o.Parent == nil && c.Op == "=" {
			v := c.Val
			if v.Kind == "param" && v.Op == "" && w.isUserInput(w.args[v.Param]) {
				return true, "", nil
			}
			other = w.sqlValue(v, w.argValues(), c.Col, o.Table)
			if v.Kind == "param" {
				otherAt = w.args[v.Param]
			}
			continue
		}
		if o.Parent != nil && c.Op == "sub" {
			if ok, o2, at2 := w.proven(c.Sub.Conds, o.Parent); ok {
				return true, "", nil
			} else if other == "" {
				other, otherAt = o2, at2
			}
		}
	}
	return false, other, otherAt
}

// copiesOwner reports whether q compares (or, an insert, sets) its own
// column named like the chain's owner column with the signed-in user: a
// copy of the owner on the child row, which proves nothing about the parent.
func (w *walker) copiesOwner(q *SQLQuery, col string) bool {
	user := func(v sqlVal) bool { return v.Kind == "param" && w.isUserInput(w.args[v.Param]) }
	for _, v := range q.Values {
		if q.Shape == "insert" && v.Col == col && user(v.Val) {
			return true
		}
	}
	for _, c := range q.Conds {
		if c.Col == col && c.Op == "=" && user(c.Val) {
			return true
		}
	}
	for _, c := range q.Where {
		if c.Col == col && c.Op == "=" && user(c.Val) {
			return true
		}
	}
	return false
}

// childWriteHint is attached to every A5 refusal of a write; {table},
// {parent}, {col} (the parent column), {at} (the annotation), {shape} (the
// SQL that proves it), {owner}, {type} and {param} (the chain's owner column,
// the user field's Go type and sqlc's Go name of the parameter) are filled in.
const childWriteHint = "Table {table} inherits its owner from {parent} through {col} ({at}), so an action that a role without the ownership bypass may call proves, in the statement that writes, that the {parent} row is the signed-in user's: {shape}. " +
	"The action passes exactly the signed-in user for sqlc.arg({owner}) (User {type} `json:\"user\" server:\"user\"`, then {param}: in.User). A copy of {owner} on the {table} row, a request field or a Public action never counts, and a claim never changes {col}. " +
	"If only administrators may do this, declare Roles with roles that bypass ownership (in cmd/server: httpx.AppRoles(...).BypassOwnership(\"admin\"))"
