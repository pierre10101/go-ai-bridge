package adapter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// internal/domain is under the grammar too (M1-M3). bridge-en renders every
// domain function's English from its body, so a change to the body (say "GBP"
// added to a currency switch) changes the English with no comment to update.
// The only hand-written English in internal/domain is a type's display name
// (`// bridge-en: <display name>`). A `bridge-en:` line on a function, a
// method, a package-level variable or any body outside M2/M3 is refused.

const phrasePrefix = "bridge-en:"

type domainType struct {
	Name, Phrase, Plural string        // Plural is optional (`// bridge-en-plural:`)
	Fields               []domainField // struct types only
	pos                  token.Position
}

const pluralPrefix = "bridge-en-plural:"

type domainField struct{ Name, JSON, Type string }

type domainFunc struct {
	Name           string
	Phrase, Not    string // rendered from the body; {param} marks an argument; Not only for predicates
	Params         []string
	Bool           bool
	asserts, calls []string // assertion reasons; domain functions it calls
	pos            token.Position
	decl           *ast.FuncDecl
	bad            bool // refused while rendering; uses are not refused again
	state          int  // 0 not rendered, 1 rendering, 2 done
}

type domainInfo struct {
	Types  map[string]*domainType
	Funcs  map[string]*domainFunc
	consts map[string]*ast.BasicLit
}

// shapeCall is the one runtime primitive a domain function may call (M3).
// bridge-en does not read its body: it has a fixed English reading that the
// tests of runtime/shape prove.
const shapeCall = "shape.Has"

const domainHint = `Allowed in internal/domain (M2): assert.Pre/Post(<cond>, "<reason>")..., then return <expr>; or, for a bool function, switch <param> { case "<text>", ...: return true } then return false`

type domainLoader struct {
	fset *token.FileSet
	info *domainInfo
	errs Refusals
}

func (l *domainLoader) refuse(n ast.Node, construct, context, hint string) {
	l.errs = append(l.errs, Refusal{Pos: l.fset.Position(n.Pos()), Construct: construct, Context: context, Hint: hint})
}

func docLine(doc *ast.CommentGroup, prefix string) string {
	if doc == nil {
		return ""
	}
	for _, c := range doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return ""
}

// hasBridgeLine reports any `// bridge-en...` doc line (bridge-en:, bridge-en not:, ...).
func hasBridgeLine(doc *ast.CommentGroup) bool {
	return docLine(doc, "bridge-en") != ""
}

func loadDomain(root string) (*domainInfo, Refusals, error) {
	fset := token.NewFileSet()
	l := &domainLoader{fset: fset, info: &domainInfo{Types: map[string]*domainType{}, Funcs: map[string]*domainFunc{}, consts: map[string]*ast.BasicLit{}}}
	rel := filepath.Join("internal", "domain")
	if _, err := os.Stat(filepath.Join(root, rel)); errors.Is(err, fs.ErrNotExist) {
		return l.info, nil, nil // an app without domain types yet
	}
	files, err := parsePkg(fset, root, rel)
	if err != nil {
		return nil, nil, err
	}
	var order []*domainFunc
	for _, f := range files {
		l.imports(f)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				l.genDecl(d)
			case *ast.FuncDecl:
				if fn := l.funcDecl(d); fn != nil {
					order = append(order, fn)
				}
			}
		}
	}
	for _, fn := range order {
		l.render(fn)
	}
	return l.info, l.errs, nil
}

// imports: internal/domain is pure. It may import fmt (M3 fmt.Sprintf), the
// runtime's assert (M2) and shape (M3 shape.Has); "time" is refused with T1
// (logic never reads the clock), everything else as I/O or hidden behaviour.
func (l *domainLoader) imports(f *ast.File) {
	for _, is := range f.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		switch {
		case is.Name != nil:
			l.refuse(is, fmt.Sprintf("renamed import %s %q", is.Name.Name, path), "internal/domain", "Import without a name")
		case path == "fmt", path == RuntimePath+"/assert", path == RuntimePath+"/shape":
		case path == "time":
			l.refuse(is, `import "time"`, "T1 time is passed in", clockHint)
		default:
			l.refuse(is, fmt.Sprintf("import %q", path), "internal/domain", domainImportsHint)
		}
	}
}

const domainImportsHint = "internal/domain is pure: it imports only fmt, " + RuntimePath + "/assert and " + RuntimePath + "/shape"

func (l *domainLoader) genDecl(d *ast.GenDecl) {
	switch d.Tok {
	case token.TYPE:
		for _, s := range d.Specs {
			ts := s.(*ast.TypeSpec)
			doc := ts.Doc
			if doc == nil && len(d.Specs) == 1 {
				doc = d.Doc
			}
			dt := &domainType{Name: ts.Name.Name, Phrase: docLine(doc, phrasePrefix), Plural: docLine(doc, pluralPrefix), pos: l.fset.Position(ts.Pos())}
			if strings.ContainsAny(dt.Phrase, "(){}") {
				l.refuse(ts, "display name "+strconv.Quote(dt.Phrase)+" of domain type "+dt.Name+" with a description", "M1 domain type",
					"`// bridge-en:` gives only the display name, a noun phrase like \"an invoice number\"; what the type means comes from code")
			}
			if dt.Plural != "" && strings.ContainsAny(dt.Plural, "(){}") {
				l.refuse(ts, "plural display name "+strconv.Quote(dt.Plural)+" of domain type "+dt.Name+" with a description", "M1 domain type",
					"`// bridge-en-plural:` gives only the plural noun phrase, with no parentheses")
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				for _, fl := range st.Fields.List {
					for _, n := range fl.Names {
						dt.Fields = append(dt.Fields, domainField{Name: n.Name, JSON: jsonTag(fl.Tag), Type: exprString(fl.Type)})
					}
				}
			}
			l.info.Types[dt.Name] = dt
		}
	case token.CONST:
		for _, s := range d.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				var lit *ast.BasicLit
				if i < len(vs.Values) {
					lit, _ = vs.Values[i].(*ast.BasicLit)
				}
				if lit == nil {
					l.refuse(n, "constant "+n.Name+" whose value is not a literal", "M3 domain expression", "Domain constants are a single integer or text literal")
					continue
				}
				l.info.consts[n.Name] = lit
			}
		}
	case token.VAR:
		for _, s := range d.Specs {
			l.refuse(s, "package-level variable "+s.(*ast.ValueSpec).Names[0].Name, "internal/domain", "Domain rules are pure functions and literal constants")
		}
	}
}

func (l *domainLoader) funcDecl(d *ast.FuncDecl) *domainFunc {
	if d.Recv != nil {
		l.refuse(d, "method "+d.Name.Name, "internal/domain", "Domain behaviour is plain functions (M2), so bridge-en can render it")
		return nil
	}
	fn := &domainFunc{Name: d.Name.Name, pos: l.fset.Position(d.Pos()), decl: d}
	for _, p := range d.Type.Params.List {
		for _, n := range p.Names {
			fn.Params = append(fn.Params, n.Name)
		}
	}
	if r := d.Type.Results; r != nil && len(r.List) == 1 && len(r.List[0].Names) == 0 && exprString(r.List[0].Type) == "bool" {
		fn.Bool = true
	}
	l.info.Funcs[fn.Name] = fn
	if hasBridgeLine(d.Doc) {
		l.refuse(d, "`bridge-en` comment on function "+fn.Name, "M2 domain function",
			"English for a domain function is rendered from its body; only a type's display name is written by hand (M1)")
	}
	return fn
}

// render fills fn.Phrase (and fn.Not for predicates) from fn's body.
func (l *domainLoader) render(fn *domainFunc) {
	switch fn.state {
	case 2:
		return
	case 1:
		l.refuse(fn.decl, "recursive call to "+fn.Name, "M2 domain function", "")
		fn.bad = true
		return
	}
	fn.state = 1
	defer func() { fn.state = 2 }()
	ctx := "domain function " + fn.Name
	n := len(l.errs)
	stmts := fn.decl.Body.List
	i := 0
	for ; i < len(stmts); i++ {
		reason, ok := l.assertion(fn, stmts[i])
		if !ok {
			break
		}
		fn.asserts = appendUnique(fn.asserts, reason)
	}
	rest := stmts[i:]
	switch {
	case len(rest) == 1 && isReturn(rest[0], 1):
		ret := rest[0].(*ast.ReturnStmt).Results[0]
		fn.Phrase = l.expr(fn, ret)
		if fn.Bool {
			fn.Not = fmt.Sprintf(t("not"), fn.Phrase)
			if x, lo, hi, ok := l.between(fn, ret); ok {
				fn.Phrase = fmt.Sprintf(dtpl("between"), x, lo, hi)
				fn.Not = fmt.Sprintf(dtpl("not between"), x, lo, hi)
			}
		}
	case len(rest) == 2 && isSwitch(rest[0]) && isReturn(rest[1], 1) && exprString(rest[1].(*ast.ReturnStmt).Results[0]) == "false":
		l.predicateSwitch(fn, rest[0].(*ast.SwitchStmt))
	case len(rest) == 0:
		l.refuse(rbrace{fn.decl.Body}, "domain function without a return", ctx, domainHint)
	default:
		s := rest[0]
		if len(rest) > 1 && isReturn(s, 1) {
			s = rest[1]
		}
		if sw, ok := s.(*ast.SwitchStmt); ok && !fn.Bool {
			l.refuse(sw, "switch in a function that does not return bool", ctx, domainHint)
		} else {
			l.refuse(s, stmtName(s), ctx, domainHint)
		}
	}
	if len(l.errs) > n {
		fn.bad = true
	}
}

func isReturn(s ast.Stmt, n int) bool {
	r, ok := s.(*ast.ReturnStmt)
	return ok && len(r.Results) == n
}

func isSwitch(s ast.Stmt) bool { _, ok := s.(*ast.SwitchStmt); return ok }

// assertion accepts assert.Pre/Post(<cond>, "<reason>") and returns the reason.
func (l *domainLoader) assertion(fn *domainFunc, s ast.Stmt) (string, bool) {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || (exprString(call.Fun) != "assert.Pre" && exprString(call.Fun) != "assert.Post") {
		return "", false
	}
	if len(call.Args) != 2 {
		l.refuse(call, "assertion with "+strconv.Itoa(len(call.Args))+" arguments", "domain function "+fn.Name, `Write assert.Pre(<cond>, "<reason>")`)
		return "", true
	}
	l.expr(fn, call.Args[0]) // the condition must be under the grammar too
	msg, ok := stringLit(call.Args[1])
	if !ok || msg == "" {
		l.refuse(call.Args[1], "assertion reason that is not a string literal", "domain function "+fn.Name, "Give a plain-English reason")
	}
	return msg, true
}

// predicateSwitch renders `switch p { case "A", "B": return true }; return false`.
func (l *domainLoader) predicateSwitch(fn *domainFunc, sw *ast.SwitchStmt) {
	ctx := "domain function " + fn.Name
	if !fn.Bool {
		l.refuse(sw, "switch in a function that does not return bool", ctx, domainHint)
		return
	}
	if sw.Init != nil || sw.Tag == nil {
		l.refuse(sw, "switch without a plain tag", ctx, domainHint)
		return
	}
	subject := l.expr(fn, sw.Tag)
	var items []string
	for _, c := range sw.Body.List {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			l.refuse(cc, "default clause", ctx, domainHint)
			return
		}
		if len(cc.Body) != 1 || !isReturn(cc.Body[0], 1) || exprString(cc.Body[0].(*ast.ReturnStmt).Results[0]) != "true" {
			l.refuse(cc, "case clause whose body is not return true", ctx, domainHint)
			return
		}
		for _, e := range cc.List {
			lit, ok := e.(*ast.BasicLit)
			if !ok || (lit.Kind != token.STRING && lit.Kind != token.INT) {
				l.refuse(e, "case value "+exprString(e)+" that is not a literal", ctx, domainHint)
				return
			}
			items = append(items, literal(lit))
		}
	}
	list := joinOr(items)
	fn.Phrase = fmt.Sprintf(dtpl("one of"), subject, list)
	fn.Not = fmt.Sprintf(dtpl("not one of"), subject, list)
}

// literal renders an integer as digits and text as a Go-quoted string.
func literal(lit *ast.BasicLit) string {
	if lit.Kind == token.STRING {
		s, _ := strconv.Unquote(lit.Value)
		return strconv.Quote(s)
	}
	return strings.ReplaceAll(lit.Value, "_", "")
}

// expr renders a domain expression (M3). Parameters become {param}.
func (l *domainLoader) expr(fn *domainFunc, e ast.Expr) string {
	ctx := "domain function " + fn.Name
	switch e := e.(type) {
	case *ast.ParenExpr:
		return fmt.Sprintf(t("paren"), l.expr(fn, e.X))
	case *ast.Ident:
		switch {
		case e.Name == "true" || e.Name == "false":
			return t(e.Name)
		case contains(fn.Params, e.Name):
			return fmt.Sprintf(dtpl("param"), e.Name)
		case l.info.consts[e.Name] != nil:
			return l.lit(l.info.consts[e.Name])
		}
		l.refuse(e, "name "+e.Name, ctx, "M3: parameters, literal constants of internal/domain, true and false")
	case *ast.BasicLit:
		return l.lit(e)
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return fmt.Sprintf(t("not"), l.expr(fn, e.X))
		}
		l.refuse(e, "unary operator "+e.Op.String(), ctx, "M3: only !")
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND, token.LOR, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return fmt.Sprintf(t(e.Op.String()), l.expr(fn, e.X), l.expr(fn, e.Y))
		}
		l.refuse(e, "arithmetic operator "+e.Op.String(), ctx, "M3: comparisons and && || ! only")
	case *ast.CallExpr:
		return l.call(fn, e)
	default:
		l.refuse(e, fmt.Sprintf("expression %s", nodeName(e)), ctx, "M3: parameters, literals, constants, comparisons, && || !, conversions, domain calls, shape.Has, fmt.Sprintf")
	}
	return "?"
}

func (l *domainLoader) lit(lit *ast.BasicLit) string {
	switch lit.Kind {
	case token.INT:
		return fmt.Sprintf(t("int"), literal(lit))
	case token.STRING:
		return fmt.Sprintf(t("string"), literal(lit))
	}
	l.refuse(lit, "literal "+lit.Value, "M3 domain expression", "Integers and text only")
	return "?"
}

func (l *domainLoader) call(fn *domainFunc, c *ast.CallExpr) string {
	ctx := "domain function " + fn.Name
	name := exprString(c.Fun)
	switch {
	case len(c.Args) == 1 && (name == "string" || name == "int64" || name == "bool" || l.info.Types[name] != nil):
		return l.expr(fn, c.Args[0]) // a conversion changes the Go type, not the value
	case name == shapeCall:
		if len(c.Args) == 2 {
			if shape, ok := stringLit(c.Args[1]); ok && shape != "" {
				return fmt.Sprintf(dtpl("shape"), l.expr(fn, c.Args[0]), shapeText(shape))
			}
		}
		l.refuse(c, "call to shape.Has without a literal shape", ctx, `M3: shape.Has(<text>, "<shape>")`)
	case name == "fmt.Sprintf":
		return l.format(fn, c)
	case l.info.Funcs[name] != nil:
		g := l.info.Funcs[name]
		l.render(g)
		if g.bad || g.Phrase == "" {
			return "?"
		}
		if len(c.Args) != len(g.Params) {
			l.refuse(c, fmt.Sprintf("call to %s with %d arguments", name, len(c.Args)), ctx, "")
			return "?"
		}
		phrase := g.Phrase
		for i, p := range g.Params {
			phrase = strings.ReplaceAll(phrase, fmt.Sprintf(dtpl("param"), p), l.expr(fn, c.Args[i]))
		}
		fn.calls = appendUnique(fn.calls, name)
		return phrase
	default:
		l.refuse(c, "call to "+name, ctx, "M3: conversions, other domain functions, shape.Has and fmt.Sprintf only")
	}
	return "?"
}

// format renders fmt.Sprintf("INV-%06d", seq) as "INV- followed by {seq} as six digits".
func (l *domainLoader) format(fn *domainFunc, c *ast.CallExpr) string {
	ctx := "domain function " + fn.Name
	f, ok := "", false
	if len(c.Args) > 0 {
		f, ok = stringLit(c.Args[0])
	}
	if !ok {
		l.refuse(c, "fmt.Sprintf without a literal format", ctx, "M3: fmt.Sprintf(\"<format>\", <expr>...)")
		return "?"
	}
	args := c.Args[1:]
	var parts []string
	text := ""
	next := 0
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			text += string(f[i])
			continue
		}
		j := i + 1
		width := 0
		if j < len(f) && f[j] == '0' {
			for j++; j < len(f) && f[j] >= '0' && f[j] <= '9'; j++ {
				width = width*10 + int(f[j]-'0')
			}
		}
		if j >= len(f) || (f[j] != 'd' && f[j] != 's') || (width > 0) != (j > i+1) || (f[j] == 's' && width > 0) || next >= len(args) {
			l.refuse(c.Args[0], "format verb "+strconv.Quote(f[i:min(j+1, len(f))]), ctx, "M3: %d, %0<n>d and %s, one argument each")
			return "?"
		}
		if text != "" {
			parts, text = append(parts, text), ""
		}
		v := l.expr(fn, args[next])
		next++
		if width > 0 {
			v = fmt.Sprintf(dtpl("padded"), v, number(width))
		}
		parts = append(parts, v)
		i = j
	}
	if text != "" {
		parts = append(parts, text)
	}
	if next != len(args) {
		l.refuse(c, fmt.Sprintf("fmt.Sprintf with %d arguments for %d verbs", len(args), next), ctx, "")
		return "?"
	}
	return strings.Join(parts, dtpl("followed by"))
}

// shapeText renders "INV-######" as "INV- followed by six digits".
func shapeText(shape string) string {
	var parts []string
	for i := 0; i < len(shape); {
		j := i
		if shape[i] == '#' {
			for j < len(shape) && shape[j] == '#' {
				j++
			}
			if j-i == 1 {
				parts = append(parts, dtpl("digit"))
			} else {
				parts = append(parts, fmt.Sprintf(dtpl("digits"), number(j-i)))
			}
		} else {
			for j < len(shape) && shape[j] != '#' {
				j++
			}
			parts = append(parts, shape[i:j])
		}
		i = j
	}
	return strings.Join(parts, dtpl("followed by"))
}

var numberWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}

func number(n int) string {
	if n >= 0 && n < len(numberWords) {
		return numberWords[n]
	}
	return strconv.Itoa(n)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func nodeName(e ast.Expr) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", e), "*ast.")
}

// assertsOf lists the assertion messages a domain function can fail, including
// those of domain functions it calls, in source order.
func (d *domainInfo) assertsOf(name string) []string {
	seen := map[string]bool{}
	var msgs []string
	var walk func(string)
	walk = func(n string) {
		fn := d.Funcs[n]
		if fn == nil || seen[n] {
			return
		}
		seen[n] = true
		for _, m := range fn.asserts {
			msgs = appendUnique(msgs, m)
		}
		for _, c := range fn.calls {
			walk(c)
		}
	}
	walk(name)
	return msgs
}

// between recognises <x> >= <lo> && <x> <= <hi> (the same x on both sides)
// so that it reads "x is between lo and hi" and, negated, "x is not between
// lo and hi" instead of "it is false that ...".
func (l *domainLoader) between(fn *domainFunc, e ast.Expr) (x, lo, hi string, ok bool) {
	and, ok1 := e.(*ast.BinaryExpr)
	if !ok1 || and.Op != token.LAND {
		return "", "", "", false
	}
	left, ok1 := and.X.(*ast.BinaryExpr)
	right, ok2 := and.Y.(*ast.BinaryExpr)
	if !ok1 || !ok2 || left.Op != token.GEQ || right.Op != token.LEQ || exprString(left.X) != exprString(right.X) {
		return "", "", "", false
	}
	return l.expr(fn, left.X), l.expr(fn, left.Y), l.expr(fn, right.Y), true
}
