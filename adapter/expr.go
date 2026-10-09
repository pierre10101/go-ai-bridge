package adapter

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

func t(key string) string { return exprTemplates[key] }

func exprString(e ast.Expr) string { return types.ExprString(e) }

// cond renders an expression in a yes/no position (guards, assertions).
func (w *walker) cond(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return fmt.Sprintf(t("paren"), w.cond(e.X))
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			if call, ok := e.X.(*ast.CallExpr); ok && w.isPageLimitCall(call) {
				return w.pageLimit(call, true)
			}
			if call, ok := e.X.(*ast.CallExpr); ok && w.isDomainCall(call) {
				return w.domainCall(call, true)
			}
			return fmt.Sprintf(t("not"), w.cond(e.X))
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND, token.LOR:
			return fmt.Sprintf(t(e.Op.String()), w.operand(e.X, e.Op), w.operand(e.Y, e.Op))
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			if s := w.countCond(e); s != "" {
				return s
			}
			if types.ExprString(e.Y) == "nil" && (e.Op == token.EQL || e.Op == token.NEQ) {
				return fmt.Sprintf(t(e.Op.String()+" nil"), w.value(e.X))
			}
			x, y := w.value(e.X), w.value(e.Y)
			if y == t("clock") && x != t("clock") {
				return clockNow(x, e.Op.String()) // T1: the same boundary words as Q6
			}
			return fmt.Sprintf(t(e.Op.String()), x, y)
		}
	case *ast.CallExpr:
		if w.isPageLimitCall(e) {
			return w.pageLimit(e, false)
		}
		if w.isDomainCall(e) {
			return w.domainCall(e, false)
		}
	case *ast.Ident, *ast.SelectorExpr:
		if s := types.ExprString(e); s != "true" && s != "false" {
			return fmt.Sprintf(t("bool name"), w.value(e))
		}
	}
	return w.value(e)
}

// countCond renders "<count> == 0" as "no customer has ..." and
// "<count> > 0" as "at least one customer has ...".
func (w *walker) countCond(e *ast.BinaryExpr) string {
	id, ok := e.X.(*ast.Ident)
	if !ok {
		return ""
	}
	loc := w.locals[id.Name]
	if loc != nil && loc.kind == "changed" {
		return w.changedCond(e, loc)
	}
	if loc != nil && loc.kind == "unknown" {
		return "?" // its query was refused already; do not refuse its comparisons too
	}
	lit, ok := e.Y.(*ast.BasicLit)
	if loc == nil || loc.kind != "count" || !ok || lit.Kind != token.INT {
		return ""
	}
	none, some, cond := "count none", "count some", loc.has
	if loc.whose { // the Q6 boundary words need "is": "`expires_at` is no later than the current time"
		none, some, cond = "count none whose", "count some whose", loc.where
	}
	switch v := lit.Value; {
	case (e.Op == token.EQL || e.Op == token.LEQ) && v == "0", e.Op == token.LSS && v == "1":
		return fmt.Sprintf(t(none), singular(loc.table), cond)
	case (e.Op == token.NEQ || e.Op == token.GTR) && v == "0", e.Op == token.GEQ && v == "1":
		return fmt.Sprintf(t(some), singular(loc.table), cond)
	}
	return ""
}

// changedCond renders a comparison on a Q6 claim's changed-row count:
// <n> != 1 (the S10 check), <n> == 1 and <n> == 0; for a claim over a Q7 IN
// list, <n> != int64(len(in.<List>)) (the S11 check) and <n> == 0. Any other
// comparison is refused. Only a guard whose entire condition is the check
// marks the claim checked (see guard and isClaimCheck).
func (w *walker) changedCond(e *ast.BinaryExpr, loc *local) string {
	if call, ok := e.Y.(*ast.CallExpr); ok {
		if name, json, ok := w.lenOfList(call); ok {
			switch {
			case !loc.multi:
				w.refuse(e, "comparison "+types.ExprString(e)+" on a single-row claim's changed-row count", "S11 multi-row claim check",
					"Only a claim over <key> IN (sqlc.slice(<name>)) (Q7) is compared with the length of a list; check a single-row claim with != 1 (S10)")
			case e.Op != token.NEQ:
				w.refuse(e, "comparison "+types.ExprString(e)+" on a claim's changed-row count", "S11 multi-row claim check",
					strings.NewReplacer("<changed>", types.ExprString(e.X), "<List>", loc.list).Replace(multiCheckHint))
			case name != loc.list:
				w.refuse(e, "comparison with the length of in."+name+", which is not the list of the claim in step "+fmt.Sprint(loc.step)+" (in."+loc.list+")",
					"S11 multi-row claim check", strings.NewReplacer("<changed>", types.ExprString(e.X), "<List>", loc.list).Replace(multiCheckHint))
			default:
				return fmt.Sprintf(t(loc.countKey("changed not len")), plural(loc.table), loc.step, plural(loc.table), fmt.Sprintf(t("request field"), json))
			}
			return "?"
		}
	}
	lit, ok := e.Y.(*ast.BasicLit)
	key := ""
	if ok && lit.Kind == token.INT {
		switch {
		case e.Op == token.NEQ && lit.Value == "1" && !loc.multi:
			key = "changed not one"
		case e.Op == token.EQL && lit.Value == "1" && !loc.multi:
			key = "changed one"
		case e.Op == token.EQL && lit.Value == "0":
			key = "changed none"
		}
	}
	if key == "" && loc.multi {
		w.refuse(e, "comparison "+types.ExprString(e)+" on a multi-row claim's changed-row count", "S11 multi-row claim check",
			"Compare the count of a claim over IN (sqlc.slice(...)) only as == 0 or in the check. "+
				strings.NewReplacer("<changed>", types.ExprString(e.X), "<List>", loc.list).Replace(multiCheckHint))
		return "?"
	}
	if key == "" {
		w.refuse(e, "comparison "+types.ExprString(e)+" on a claim's changed-row count", "S10 claim check",
			"Compare a Q6 result only as != 1 (the check), == 1 or == 0")
		return "?"
	}
	return fmt.Sprintf(t(loc.countKey(key)), singular(loc.table), loc.step)
}

// listInput matches in.<Field> where Field is a D10 list input.
func (w *walker) listInput(e ast.Expr) (name, json string, ok bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	root, ok := sel.X.(*ast.Ident)
	if !ok || w.locals[root.Name] == nil || w.locals[root.Name].kind != "request" {
		return "", "", false
	}
	for _, f := range w.f.Input {
		if f.Name == sel.Sel.Name && f.List != "" {
			return f.Name, f.JSON, true
		}
	}
	return "", "", false
}

// lenOfList matches int64(len(in.<List>)), the only place len is allowed (S11).
func (w *walker) lenOfList(c *ast.CallExpr) (name, json string, ok bool) {
	if types.ExprString(c.Fun) != "int64" || len(c.Args) != 1 {
		return "", "", false
	}
	inner, ok := c.Args[0].(*ast.CallExpr)
	if !ok || types.ExprString(inner.Fun) != "len" || len(inner.Args) != 1 {
		return "", "", false
	}
	return w.listInput(inner.Args[0])
}

// operand wraps a mixed &&/|| sub-expression in parentheses.
func (w *walker) operand(e ast.Expr, parent token.Token) string {
	if b, ok := e.(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) && b.Op != parent {
		return fmt.Sprintf(t("paren"), w.cond(e))
	}
	return w.cond(e)
}

// value renders an expression in a value position. It is the only place
// expressions are accepted; anything not listed here is refused.
func (w *walker) value(e ast.Expr) string {
	const ctx = "expression"
	switch e := e.(type) {
	case *ast.ParenExpr:
		return fmt.Sprintf(t("paren"), w.value(e.X))
	case *ast.Ident:
		switch e.Name {
		case "nil", "true", "false":
			return t(e.Name)
		}
		if loc := w.locals[e.Name]; loc != nil {
			return w.localName(e.Name, loc)
		}
		w.refuse(e, "name "+e.Name+" that is not a local", ctx, "E1: only a, ctx, in, query results and lets")
	case *ast.SelectorExpr:
		if s, ok := w.selector(e); ok {
			return s
		}
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			return fmt.Sprintf(t("int"), e.Value)
		case token.STRING:
			return fmt.Sprintf(t("string"), e.Value)
		}
		w.refuse(e, "literal "+e.Value, ctx, "E2: integers and strings only")
	case *ast.CallExpr:
		if w.isPageLimitCall(e) {
			return w.pageLimit(e, false)
		}
		if w.isDomainCall(e) {
			return w.domainCall(e, false)
		}
		if readsClock(e.Fun) {
			w.refuse(e, "clock read "+types.ExprString(e.Fun), "T1 time is passed in", clockHint)
			return "?"
		}
		w.refuse(e, "call to "+types.ExprString(e.Fun), ctx, "E5: only domain.<Func>(...) and page.IsPageLimit(...) calls; queries go through S3; len only as int64(len(in.<List>)) in the S11 check")
	case *ast.CompositeLit:
		typ := w.recordType(e)
		if typ == "" {
			return "?"
		}
		if !strings.HasPrefix(typ, "domain.") {
			w.refuse(e, "record of type "+typ+" inside an expression", "E6 record",
				"Build Output in an S5 let and db.<Query>Params as the query argument")
			return "?"
		}
		name := strings.TrimPrefix(typ, "domain.")
		phrase := w.domainTypePhrase(e, name, false)
		fields := w.domainFields(e, name)
		if len(fields) == 0 {
			return fmt.Sprintf(t("empty record"), phrase)
		}
		return fmt.Sprintf(t("record"), phrase, joinList(fields))
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND, token.LOR, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return w.cond(e)
		}
		w.refuse(e, "arithmetic operator "+e.Op.String(), ctx, "Do arithmetic in internal/domain and call it (E5)")
	case *ast.UnaryExpr:
		switch {
		case e.Op == token.NOT:
			return w.cond(e)
		case e.Op == token.SUB && isIntLit(e.X):
			return "-" + e.X.(*ast.BasicLit).Value
		}
		w.refuse(e, "unary operator "+e.Op.String(), ctx, "E4: only !")
	case *ast.FuncLit:
		w.refuse(e, "function literal (closure)", ctx, "Name the rule in internal/domain and call it (E5)")
	case *ast.IndexExpr, *ast.IndexListExpr:
		w.refuse(e, "index expression", ctx, "No collections in actions")
	case *ast.SliceExpr:
		w.refuse(e, "slice expression", ctx, "No collections in actions")
	case *ast.TypeAssertExpr:
		w.refuse(e, "type assertion", ctx, "")
	case *ast.StarExpr:
		w.refuse(e, "pointer dereference", ctx, "")
	default:
		w.refuse(e, fmt.Sprintf("expression %T", e), ctx, "")
	}
	return "?"
}

// readsClock reports a call rooted at package time (time.Now().Unix(), time.Since(...)).
func readsClock(fun ast.Expr) bool {
	for {
		switch x := fun.(type) {
		case *ast.SelectorExpr:
			fun = x.X
		case *ast.CallExpr:
			fun = x.Fun
		case *ast.Ident:
			return x.Name == "time"
		default:
			return false
		}
	}
}

func isIntLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.INT
}

// localName is how the English refers to a local: never by its Go name,
// except S5 lets of plain values, which the Steps introduce by name.
func (w *walker) localName(name string, loc *local) string {
	switch loc.kind {
	case "request":
		return t("request")
	case "context":
		return t("context")
	case "action":
		return t("action")
	case "count":
		return fmt.Sprintf(t("count value"), plural(loc.table), loc.where)
	case "changed":
		return fmt.Sprintf(t(loc.countKey("changed value")), plural(loc.table), loc.step)
	case "row":
		return fmt.Sprintf(t("row"), loc.phrase)
	case "rows":
		return fmt.Sprintf(t("listed rows"), plural(loc.table))
	case "list":
		return fmt.Sprintf(t("list"), pluralPhrase(loc.phrase), loc.step)
	case "cursor":
		return t("next cursor")
	case "answer":
		return t("answer")
	case "unknown":
		return "?"
	case "index":
		return "?"
	}
	return fmt.Sprintf(t("let"), name)
}

// selector accepts <local>.<Field>[.<Field>] (E1) and names it in English.
func (w *walker) selector(e *ast.SelectorExpr) (string, bool) {
	var path []string
	var x ast.Expr = e
	for {
		sel, ok := x.(*ast.SelectorExpr)
		if !ok {
			break
		}
		path = append([]string{sel.Sel.Name}, path...)
		x = sel.X
	}
	root, ok := x.(*ast.Ident)
	switch {
	case !ok:
		w.refuse(e, "selector on "+types.ExprString(x), "expression", "E1: <local>.<Field>")
		return "", false
	case w.locals[root.Name] == nil:
		w.refuse(e, "package-level value "+types.ExprString(e), "expression", "E5: call domain functions; no package-level values")
		return "", false
	case len(path) > 2:
		w.refuse(e, "selector chain "+types.ExprString(e), "expression", "E1: at most <local>.<Field>.<Field>")
		return "", false
	}
	loc := w.locals[root.Name]
	if loc.kind == "unknown" {
		return "?", true // its query was refused already
	}
	bad := func() (string, bool) {
		w.refuse(e, "field "+types.ExprString(e)+" that bridge-en cannot name", "expression",
			"E1: request and answer fields, columns a query returns, and fields of domain records")
		return "", false
	}
	switch loc.kind {
	case "request", "answer":
		fields := w.f.Input
		tmpl := "request field"
		if loc.kind == "answer" {
			fields, tmpl = w.f.Output, "answer field"
		}
		json, ok := w.jsonPath(fields, path)
		if !ok {
			return bad()
		}
		if loc.kind == "request" && len(path) == 1 {
			for _, f := range fields {
				if f.Name == path[0] && f.ServerSet != "" {
					return t(f.ServerSet), true // "the current time" (T1), "the session" (T2)
				}
				if f.Name == path[0] && f.List != "" {
					w.refuse(e, "list input "+types.ExprString(e)+" used as a value", "D10 list input", listUseHint)
					return "", false
				}
			}
		}
		return fmt.Sprintf(t(tmpl), json), true
	case "action":
		if len(path) == 1 && path[0] == "q" {
			return t("queries"), true
		}
	case "row":
		if col := matchName(loc.cols, path[0]); col != "" && len(path) == 1 {
			return fmt.Sprintf(t("row field"), loc.phrase, col), true
		}
	case "list":
		if len(path) == 0 {
			return fmt.Sprintf(t("list"), pluralPhrase(loc.phrase), loc.step), true
		}
	case "let":
		if dt := w.env.domain.Types[loc.typ]; dt != nil && len(path) == 1 {
			for _, f := range dt.Fields {
				if f.Name == path[0] && f.JSON != "" {
					return fmt.Sprintf(t("let field"), f.JSON, root.Name), true
				}
			}
		}
	}
	return bad()
}

// jsonPath turns Total.Cents into "total.cents" using json tags of the
// Input/Output fields and of the domain struct they hold.
func (w *walker) jsonPath(fields []Field, path []string) (string, bool) {
	for _, f := range fields {
		if f.Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return f.JSON, true
		}
		if dt := w.env.domain.Types[f.Domain]; dt != nil {
			for _, df := range dt.Fields {
				if df.Name == path[1] && df.JSON != "" {
					return f.JSON + "." + df.JSON, true
				}
			}
		}
	}
	return "", false
}

func (w *walker) isDomainCall(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	return ok && types.ExprString(sel.X) == "domain" && w.pkgs["domain"]
}

// pageLimitCall is the runtime primitive that guards a Q5 page size (E5).
const pageLimitCall = "page.IsPageLimit"

func (w *walker) isPageLimitCall(c *ast.CallExpr) bool {
	return types.ExprString(c.Fun) == pageLimitCall && w.pkgs["page"]
}

// pageLimit renders page.IsPageLimit(<value>): bridge-en does not read its
// body; it reads "<value> is between 1 and page.MaxPageSize (both included)",
// which the tests of runtime/page prove.
func (w *walker) pageLimit(c *ast.CallExpr, negated bool) string {
	if len(c.Args) != 1 {
		w.refuse(c, fmt.Sprintf("call to %s with %d arguments", pageLimitCall, len(c.Args)), "E5 domain call", "Write page.IsPageLimit(<value>)")
		return "?"
	}
	max, _ := pageSizes()
	key := "between"
	if negated {
		key = "not between"
	}
	return fmt.Sprintf(dtpl(key), w.value(c.Args[0]), "1", strconv.Itoa(max))
}

func (w *walker) callName(c *ast.CallExpr) string { return c.Fun.(*ast.SelectorExpr).Sel.Name }

// domainCall fills the English rendered from the domain function's body
// (M2) with the rendered arguments, and records the assertions it can fail.
func (w *walker) domainCall(c *ast.CallExpr, negated bool) string {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = w.value(a)
	}
	name := w.callName(c)
	fn := w.env.domain.Funcs[name]
	if fn == nil {
		w.refuse(c, "domain."+name+", which internal/domain does not declare as a function", "E5 domain call", "")
		return "?"
	}
	if fn.bad {
		return "?" // its body was refused in internal/domain already
	}
	phrase := fn.Phrase
	if negated {
		if !fn.Bool {
			w.refuse(c, "! on domain."+name+", which does not return bool", "E4 logic", "")
			return "?"
		}
		phrase = fn.Not
	}
	for _, m := range w.env.domain.assertsOf(name) {
		w.asserts = appendUnique(w.asserts, m)
	}
	if len(args) != len(fn.Params) {
		w.refuse(c, fmt.Sprintf("call to domain.%s with %d arguments", name, len(args)), "E5 domain call", "")
		return "?"
	}
	for i, p := range fn.Params {
		ph := fmt.Sprintf(dtpl("param"), p)
		if !strings.Contains(phrase, ph) {
			w.refuse(c, "domain."+name+", whose body never reads "+p, "M2 domain function", "Every argument appears in the English; drop the unused parameter")
			return "?"
		}
		if args[i] == t("clock") {
			phrase = clockArg(phrase, ph) // T1: the same boundary words as Q6
		}
		phrase = strings.ReplaceAll(phrase, ph, args[i])
	}
	return phrase
}

// clockArg rewrites the comparisons of a domain function's English whose
// right operand is the parameter placeholder ph, when the call passes the
// current time for it: "{e} is at most {now}" becomes "{e} is no later than
// {now}", and so on for every operator (see clockNow).
func clockArg(phrase, ph string) string {
	for goOp, sqlOp := range map[string]string{"==": "=", "!=": "<>", "<": "<", "<=": "<=", ">": ">", ">=": ">="} {
		phrase = strings.ReplaceAll(phrase, fmt.Sprintf(t(goOp), "", ph), fmt.Sprintf(t("clock "+sqlOp), "", ph))
	}
	return phrase
}

// recordType accepts Output, domain.<T> and db.<T>Params (E6).
func (w *walker) recordType(lit *ast.CompositeLit) string {
	if lit.Type != nil {
		s := types.ExprString(lit.Type)
		switch {
		case s == "Output",
			strings.HasPrefix(s, "domain.") && w.pkgs["domain"],
			strings.HasPrefix(s, "db.") && strings.HasSuffix(s, "Params") && w.pkgs["db"]:
			return s
		}
		w.refuse(lit, "record of type "+s, "E6 record", "Allowed: Output, domain.<T>, db.<T>Params")
		return ""
	}
	w.refuse(lit, "record without a type", "E6 record", "")
	return ""
}

// domainFields renders the keyed fields of a domain.<T> record by their json names.
func (w *walker) domainFields(lit *ast.CompositeLit, typ string) []string {
	dt := w.env.domain.Types[typ]
	var out []string
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			w.refuse(el, "unkeyed record field", "E6 record", "Write Field: value")
			continue
		}
		key := types.ExprString(kv.Key)
		json := ""
		if dt != nil {
			for _, f := range dt.Fields {
				if f.Name == key {
					json = f.JSON
				}
			}
		}
		if json == "" {
			w.refuse(kv, "field "+key+" of domain."+typ+" without a json tag", "M1 domain type", "Give domain struct fields json tags; the English names them that way")
			continue
		}
		out = append(out, fmt.Sprintf(t("field"), json, w.value(kv.Value)))
	}
	return out
}

func joinList(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// pluralPhrase turns a display name ("an invoice summary") into its plural
// ("invoice summaries").
func pluralPhrase(phrase string) string {
	for _, art := range []string{"a ", "an "} {
		phrase = strings.TrimPrefix(phrase, art)
	}
	switch {
	case strings.HasSuffix(phrase, "y") && !strings.HasSuffix(phrase, "ey"):
		return strings.TrimSuffix(phrase, "y") + "ies"
	case strings.HasSuffix(phrase, "s"):
		return phrase + "es"
	}
	return phrase + "s"
}
