package adapter

import (
	"fmt"
	"go/ast"
	"strings"
)

// A4 ownership. A table that schema.sql declares owned (`-- owner: <col>`,
// loadOwners) is written only in the signed-in user's name by any action
// that a role without the ownership bypass may call: an UPDATE (Q6 claim)
// has <col> = <the server:"user" field> as an AND condition of its WHERE, an
// INSERT (Q3) sets <col> to it, and neither sets <col> to anything else. A
// Public action never writes an owned table. An action whose Roles lists
// only roles that cmd/server marks with
// httpx.AppRoles(...).BypassOwnership(...) bypasses this. Reads are not
// refused (an owned row may be readable by others, for example a public
// list of events); the English of every read of an owned table says whether
// it is limited to the caller's own rows.

// ownership applies A4 to query q, called at statement s, and returns the
// sentence its step says about ownership ("" when q's table is not owned).
func (w *walker) ownership(s ast.Stmt, q *SQLQuery) string {
	o := w.env.owners[q.Table]
	if o == nil {
		return ""
	}
	user := w.userField()
	if user != nil && user.goType != o.GoType && !w.ownerTypeSeen {
		w.ownerTypeSeen = true
		col := map[string]string{"int64": "INTEGER", "string": "TEXT"}[o.GoType]
		w.errs = append(w.errs, Refusal{Pos: user.pos, Construct: fmt.Sprintf("signed-in user field %s of type %s for table %s, whose owner column %s is %s (%s)", user.Name, user.goType, q.Table, o.Col, col, o.pos),
			Context: "A4 ownership", Hint: fmt.Sprintf("The signed-in user is compared with the owner column, so they have the same type: User %s `json:\"user\" server:\"user\"` for %s owner column (or change the column's type in schema.sql)", o.GoType, map[string]string{"INTEGER": "an INTEGER", "TEXT": "a TEXT"}[col])})
	}
	rows, row := plural(q.Table), singular(q.Table)
	scoped, other, otherAt := w.ownerScope(q, o.Col)
	write := len(q.Writes) > 0
	if !write {
		if scoped {
			return fmt.Sprintf(t("owned read"), rows, o.Col)
		}
		return fmt.Sprintf(t("unowned read"), rows, o.Col)
	}
	if bypass := w.bypassRoles(); bypass != nil {
		if scoped {
			return w.ownedWrite(q, rows, row, o.Col)
		}
		key := "bypass write"
		if len(bypass) > 1 {
			key = "bypass write n"
		}
		return fmt.Sprintf(t(key), rows, o.Col, joinOr(bypass))
	}
	hint := strings.NewReplacer("{table}", q.Table, "{col}", o.Col, "{at}", o.pos.String(), "{type}", o.GoType, "{param}", sqlcField(o.Col)).Replace(ownerWriteHint)
	refuse := func(n ast.Node, construct string) {
		if n == nil {
			n = s
		}
		w.refuse(n, construct, "A4 ownership", hint)
	}
	switch {
	case w.f.Public:
		refuse(s, fmt.Sprintf("write to owned table %s (query %s) in a Public action", q.Table, q.Name))
	case user == nil:
		refuse(s, fmt.Sprintf("write to owned table %s (query %s) in an action without the signed-in user", q.Table, q.Name))
	case other != "":
		refuse(otherAt, fmt.Sprintf("write to owned table %s (query %s) whose owner column %s is %s, which is not the signed-in user", q.Table, q.Name, o.Col, other))
	case !scoped && q.Shape == "insert":
		refuse(s, fmt.Sprintf("insert into owned table %s (query %s) that does not set its owner column %s to the signed-in user", q.Table, q.Name, o.Col))
	case !scoped:
		refuse(s, fmt.Sprintf("write to owned table %s (query %s) whose WHERE does not limit it to rows the signed-in user owns (%s = the signed-in user)", q.Table, q.Name, o.Col))
	}
	if q.Shape == "claim" {
		for _, v := range q.Values {
			if v.Col == o.Col && !(v.Val.Kind == "param" && v.Val.Op == "" && w.isUserInput(w.args[v.Val.Param])) {
				refuse(s, fmt.Sprintf("change of the owner column %s of owned table %s (query %s)", o.Col, q.Table, q.Name))
			}
		}
	}
	return w.ownedWrite(q, rows, row, o.Col)
}

// ownedWrite is the sentence of a write limited to the caller's own rows.
func (w *walker) ownedWrite(q *SQLQuery, rows, row, col string) string {
	if q.Shape == "insert" {
		return fmt.Sprintf(t("owned insert"), row, col)
	}
	return fmt.Sprintf(t("owned update"), rows, col)
}

// ownerScope reports whether q limits its rows to the signed-in user's
// (WHERE <col> = <the server:"user" field>, an AND condition outside any OR
// group) or, for an insert, sets <col> to it. other is the English of what
// <col> is compared with or set to instead, at otherAt.
func (w *walker) ownerScope(q *SQLQuery, col string) (scoped bool, other string, otherAt ast.Node) {
	check := func(v sqlVal) {
		if v.Kind == "param" && v.Op == "" && w.isUserInput(w.args[v.Param]) {
			scoped = true
			return
		}
		if other == "" {
			other = w.sqlValue(v, w.argValues(), col, q.Table)
			if v.Kind == "param" {
				otherAt = w.args[v.Param]
			}
		}
	}
	switch q.Shape {
	case "claim":
		for _, c := range q.Conds {
			if c.Any == nil && c.Col == col && c.Op == "=" {
				check(c.Val)
			}
		}
	case "insert":
		for _, v := range q.Values {
			if v.Col == col {
				check(v.Val)
			}
		}
	default:
		for _, c := range q.Where {
			if c.Col == col && c.Op == "=" {
				check(c.Val)
			}
		}
	}
	if scoped {
		other = ""
	}
	return scoped, other, otherAt
}

// argValues renders the Go value of every parameter of the current query.
func (w *walker) argValues() map[string]string {
	vals := map[string]string{}
	for p, e := range w.args {
		vals[p] = w.value(e)
	}
	return vals
}

// bypassRoles is the action's Roles when every one of them bypasses
// ownership (A4, BypassOwnership in cmd/server), or nil.
func (w *walker) bypassRoles() []string {
	if w.f.Public || len(w.f.Roles) == 0 || w.env.roles == nil {
		return nil
	}
	names := make([]string, len(w.f.Roles))
	for i, r := range w.f.Roles {
		if !w.env.roles.passes[r] {
			return nil
		}
		names[i] = "`" + r + "`"
	}
	return names
}

// userField is the Input field tagged server:"user" (T3), or nil.
func (w *walker) userField() *Field {
	for i := range w.f.Input {
		if w.f.Input[i].ServerSet == "user" {
			return &w.f.Input[i]
		}
	}
	return nil
}

// isUserInput reports whether e is exactly in.<Field> for the Input field
// tagged server:"user" (T3): the only value that scopes an owned table.
func (w *walker) isUserInput(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	root, ok := sel.X.(*ast.Ident)
	if !ok || w.locals[root.Name] == nil || w.locals[root.Name].kind != "request" {
		return false
	}
	u := w.userField()
	return u != nil && u.Name == sel.Sel.Name
}

// ownerWriteHint is attached to every A4 refusal of a write; {table},
// {col}, {at} (the annotation in schema.sql), {param} (sqlc's Go name of it) and {type} (the column's Go
// type) are filled in.
const ownerWriteHint = "Table {table} is owned by {col} ({at}), so an action that a role without the ownership bypass may call writes only rows the signed-in user owns: " +
	"an UPDATE has {col} = sqlc.arg({col}) as an AND condition of its WHERE (not inside an OR group) and sets {col} to nothing else, and an INSERT sets {col} = sqlc.arg({col}); " +
	"the action passes exactly the signed-in user for it (User {type} `json:\"user\" server:\"user\"`, then {param}: in.User). A request field never counts, and a Public action never writes an owned table. " +
	"If only administrators may do this, declare Roles with roles that bypass ownership (in cmd/server: httpx.AppRoles(...).BypassOwnership(\"admin\"))"

// sqlcField is the Go field name sqlc gives parameter p in <Query>Params
// (organizer_id -> OrganizerID: sqlc writes the part "id" as ID).
func sqlcField(p string) string {
	parts := strings.Split(p, "_")
	for i, s := range parts {
		if s == "id" {
			parts[i] = "ID"
		} else if s != "" {
			parts[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(parts, "")
}
