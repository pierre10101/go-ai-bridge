package adapter

import (
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

// Field is one Input/Output field.
type Field struct {
	Name, JSON, Type, Line string
	Domain                 string // domain type name, for domain.<T> fields
	ServerSet              string // "clock" (T1, `clock:"now"`), "session" (T2, `server:"session"`), "user" or "role" (T3): filled by httpx, never sent
	List                   string // D10 list input: the element type (int64 or string); "" for any other field
	goType                 string // the Go type as written (int64, string, ...)
	pos                    token.Position
}

// FailureCase is a D6 declaration plus every S2 guard that raises it.
type FailureCase struct {
	ID, Message, StatusText, Line string
	Status                        int
	pos                           token.Position
	steps                         []int
	when                          int // the worst of its steps: whenBefore .. whenWrote (see handle.go)
}

// QueryCall is one S3 statement.
type QueryCall struct {
	Name string
	pos  token.Position
}

// Step is one numbered step of Handle, in code order.
type Step struct {
	N       int
	Text    string
	Bullets []string
	Notes   []string
	kind    string
}

// Feature is everything the English templates need for one slice.
type Feature struct {
	Dir, Package, Title, Method, Path                          string
	Input, Output                                              []Field
	BodyInput, ServerSet                                       []Field  // Input split: sent by the caller / set by the server (T1, T2, T3)
	Public                                                     bool     // A1: var Roles = httpx.Public
	Roles                                                      []string // A1: var Roles = httpx.Roles(...), in declared order
	AccessLine                                                 string   // A1: who may call it (httpx.PublicRule or httpx.RolesRule)
	rolesSeen                                                  bool
	ServerIntro                                                string
	InputCount, InputIntro, InputRule, ErrorShape, SuccessLine string
	DataLine, NoPre, InternalStatus, TxLine                    string
	Answers                                                    []string
	Pre                                                        []string
	Steps                                                      []*Step
	Failures                                                   []*FailureCase
	Queries                                                    []*QueryCall
	reads, writes                                              []string
	queryFailSteps, assertSteps                                []int
	commitStep                                                 int // the success answer that commits the transaction; 0 if no query
	hasPageQuery, pageLimitGuarded, hasNext                    bool
	nextField                                                  string // the answer field that carries the next cursor
	env                                                        *env
}

var (
	snakeRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	routeRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) (/[a-z0-9_{}/-]*)$`)
	fidRe   = regexp.MustCompile(`^F[1-9][0-9]*$`)
	roleRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// statusCodes is the closed list of statuses a failure case may use.
var statusCodes = map[string]int{
	"StatusBadRequest": 400, "StatusUnauthorized": 401, "StatusForbidden": 403,
	"StatusNotFound": 404, "StatusConflict": 409, "StatusGone": 410,
	"StatusUnprocessableEntity": 422, "StatusTooManyRequests": 429,
}

type walker struct {
	fset          *token.FileSet
	env           *env
	f             *Feature
	errs          Refusals
	locals        map[string]*local
	fids          map[string]*FailureCase
	pkgs          map[string]bool
	writes        []int               // step numbers of writes so far
	wrote         map[int]string      // write step -> what it may have changed by now: wroteSome or wroteMaybe
	readAt        map[string]int      // W1: table -> step of the first read query (Q1, Q2, Q5) of it
	sliceField    string              // the list input (Go field name) passed to the current query's Q7 IN list
	args          map[string]ast.Expr // the Go value passed for each SQL parameter of the current query
	asserts       []string            // domain assertions met while rendering the current statement
	panics        bool                // a domain function met in the current statement calls panic
	ownerTypeSeen bool                // A4: the user field's type mismatch is refused once
}

// local is a name defined in Handle and how the English refers to it.
type local struct {
	kind    string // request, context, action, count, row, answer, let
	table   string // count, row
	where   string // count: "`id` is ..."
	has     string // count: "`id` equal to ..."
	whose   bool   // count: compares with the current time, so a guard says "there is no <row> whose <where>"
	phrase  string // row: "new invoice"
	cols    []string
	typ     string // let: domain type name of a record
	lit     *ast.CompositeLit
	limit   string   // rows: the English of the page query's LIMIT value
	step    int      // list: the step that built it; changed: the claim step
	stmt    ast.Node // changed: the Q6 query statement (S10 refusals point here)
	checked bool     // changed: a guard whose entire condition is the S10 (or S11) check
	multi   bool     // changed: a claim over <key> IN (sqlc.slice(...)) (Q7), checked by S11
	list    string   // changed, multi: the list input (Go field name) its IN list is bound to
	nested  int      // changed: the line of the first guard that tests the check inside a compound condition
}

// ParseAction parses <dir>/action.go and <dir>/queries/*.sql, reads the
// app's internal/domain and the runtime's httpx sentences, and accepts only
// the Grammar.
func ParseAction(dir string) (*Feature, error) {
	path := filepath.Join(dir, "action.go")
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, path, src, goparser.ParseComments)
	if err != nil {
		return nil, err
	}
	e, sqlErrs, err := loadEnv(dir)
	if err != nil {
		return nil, err
	}
	w := &walker{fset: fset, env: e, f: &Feature{Dir: dir, env: e}, fids: map[string]*FailureCase{}, pkgs: map[string]bool{}, readAt: map[string]int{}, wrote: map[int]string{}}
	w.errs = append(w.errs, sqlErrs...)
	w.decls(file)
	if len(w.errs) > 0 {
		return nil, w.errs
	}
	w.finish()
	if len(w.errs) > 0 {
		return nil, w.errs
	}
	return w.f, nil
}

func (w *walker) refuse(n ast.Node, construct, context, hint string) {
	w.errs = append(w.errs, Refusal{Pos: w.fset.Position(n.Pos()), Construct: construct, Context: context, Hint: hint})
}

func (w *walker) decls(file *ast.File) {
	pkg := file.Name.Name
	if !snakeRe.MatchString(pkg) {
		w.refuse(file.Name, fmt.Sprintf("package name %q", pkg), "D1 package", "Use snake_case")
	}
	w.f.Package, w.f.Title = pkg, humanize(pkg)
	if n := w.fset.File(file.Pos()).LineCount(); n > MaxFileLines {
		w.refuse(file.Name, fmt.Sprintf("action.go of %d lines", n), "hard limit", fmt.Sprintf("At most %d lines", MaxFileLines))
	}
	seen := map[string]bool{}
	var handle *ast.FuncDecl
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			w.genDecl(pkg, d, seen)
		case *ast.FuncDecl:
			switch {
			case d.Recv == nil && d.Name.Name == "New":
				w.constructor(d)
			case d.Recv != nil && d.Name.Name == "Handle":
				handle = d
			default:
				w.refuse(d, "function "+d.Name.Name, "top level of action.go", "Only New (D8) and Handle (D9); put pure helpers in internal/domain")
				continue
			}
			seen[d.Name.Name] = true
		}
	}
	if !w.f.rolesSeen {
		w.refuse(file.Name, "action.go without a Roles declaration", "A1 who may call it", rolesHint)
	}
	if handle != nil {
		w.handle(handle) // last, so every F-ID and import is known
	}
	for _, req := range [][2]string{{"Route", "D3 route"}, {"Input", "D4 input"}, {"Output", "D5 output"},
		{"Action", "D7 action"}, {"New", "D8 constructor"}, {"Handle", "D9 handle"}} {
		if !seen[req[0]] {
			w.refuse(file.Name, "action.go without "+req[0], req[1]+" is required", "")
		}
	}
}

func (w *walker) genDecl(pkg string, d *ast.GenDecl, seen map[string]bool) {
	switch d.Tok {
	case token.IMPORT:
		for _, s := range d.Specs {
			w.importSpec(pkg, s.(*ast.ImportSpec))
		}
	case token.CONST:
		if w.route(d) {
			seen["Route"] = true
		}
	case token.TYPE:
		for _, s := range d.Specs {
			ts := s.(*ast.TypeSpec)
			switch ts.Name.Name {
			case "Input":
				w.f.Input = w.fields(ts, "D4 input", true)
			case "Output":
				w.f.Output = w.fields(ts, "D5 output", false)
			case "Action":
				w.actionStruct(ts)
			default:
				w.refuse(ts, "type "+ts.Name.Name, "top level of action.go", "Allowed types: Input (D4), Output (D5), Action (D7); shared types go in internal/domain")
				continue
			}
			seen[ts.Name.Name] = true
		}
	case token.VAR:
		for _, s := range d.Specs {
			vs := s.(*ast.ValueSpec)
			if len(vs.Names) == 1 && vs.Names[0].Name == "Roles" {
				w.rolesDecl(vs)
				continue
			}
			w.failureDecl(vs)
		}
	}
}

func (w *walker) importSpec(pkg string, is *ast.ImportSpec) {
	path, _ := strconv.Unquote(is.Path.Value)
	if is.Name != nil {
		w.refuse(is, fmt.Sprintf("renamed import %s %q", is.Name.Name, path), "D2 imports", "Import without a name")
		return
	}
	name := ""
	switch {
	case path == "context":
		name = "context"
	case path == "net/http":
		name = "http"
	case path == "time":
		w.refuse(is, `import "time"`, "T1 time is passed in", clockHint)
		return
	case path == RuntimePath+"/assert":
		name = "assert"
	case path == RuntimePath+"/failure":
		name = "failure"
	case path == RuntimePath+"/page":
		name = "page"
	case path == RuntimePath+"/httpx":
		name = "httpx" // only for var Roles (A1); Handle never uses it
	case strings.HasSuffix(path, "/internal/domain"):
		name = "domain"
	case strings.HasSuffix(path, "/features/"+pkg+"/db"):
		name = "db"
	}
	if name == "" {
		w.refuse(is, fmt.Sprintf("import %q", path), "D2 imports",
			"Allowed: context, net/http, "+RuntimePath+"/{assert,failure,page,httpx}, <module>/internal/domain, <module>/features/"+pkg+"/db")
		return
	}
	w.pkgs[name] = true
}

func (w *walker) route(d *ast.GenDecl) bool {
	if len(d.Specs) == 1 {
		vs := d.Specs[0].(*ast.ValueSpec)
		if len(vs.Names) == 1 && vs.Names[0].Name == "Route" && vs.Type == nil && len(vs.Values) == 1 {
			if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, _ := strconv.Unquote(lit.Value)
				if m := routeRe.FindStringSubmatch(s); m != nil {
					w.f.Method, w.f.Path = m[1], m[2]
					return true
				}
			}
		}
	}
	w.refuse(d, "constant declaration "+types.ExprString(d.Specs[0].(*ast.ValueSpec).Names[0]), "D3 route",
		`The only constant is Route = "<METHOD> /<path>"`)
	return false
}

func (w *walker) fields(ts *ast.TypeSpec, ctx string, limit bool) []Field {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		w.refuse(ts, "type "+ts.Name.Name+" that is not a struct", ctx, "")
		return nil
	}
	var out []Field
	for _, fl := range st.Fields.List {
		if len(fl.Names) != 1 {
			w.refuse(fl, "embedded or grouped field", ctx, "One named field per line")
			continue
		}
		name := fl.Names[0].Name
		var typ, dom, list string
		listTag, hasList := structTagOK(fl.Tag, "list")
		if at, ok := fl.Type.(*ast.ArrayType); ok && at.Len == nil && ctx == "D4 input" {
			typ, list = w.listType(fl, name, at, listTag, hasList)
		} else {
			typ, dom = w.fieldType(fl.Type, ctx)
			switch {
			case hasList && ctx != "D4 input":
				w.refuse(fl, "list tag on "+name+" outside Input", "D10 list input", "Only an Input field can be a list input")
			case hasList:
				w.refuse(fl, "list tag on "+name+" of type "+types.ExprString(fl.Type), "D10 list input", listHint)
			}
		}
		tag := jsonTag(fl.Tag)
		if tag == "" {
			w.refuse(fl, "field "+name+" without a json tag", ctx, "Add `json:\"snake_name\"`")
			continue
		}
		line := fmt.Sprintf(docSentences["field"], tag, typ)
		server := ""
		if c := structTag(fl.Tag, "clock"); c != "" {
			switch {
			case ctx != "D4 input":
				w.refuse(fl, "clock tag on "+name+" outside Input", "T1 time is passed in", "Only an Input field can be the current time")
			case c != "now" || types.ExprString(fl.Type) != "int64":
				w.refuse(fl, "clock field "+name+" that is not int64 tagged clock:\"now\"", "T1 time is passed in",
					"Write: Now int64 `json:\"now\" clock:\"now\"` (whole seconds since 1970-01-01 UTC, set by the server)")
			}
			server = "clock"
			line = fmt.Sprintf(docSentences["field"], tag, w.env.http.ClockRule)
		}
		if sv := structTag(fl.Tag, "server"); sv != "" {
			typName := types.ExprString(fl.Type)
			ctxRule := "T2 session is passed in"
			if sv == "user" || sv == "role" {
				ctxRule = "T3 user, role passed in"
			}
			value, typed := w.env.http.SessionValue[typName]
			switch {
			case ctx != "D4 input":
				w.refuse(fl, "server tag on "+name+" outside Input", ctxRule, "Only an Input field can be set by the server")
			case server != "":
				w.refuse(fl, "field "+name+" tagged both clock and server", ctxRule, "A server-set field is either the current time (T1), the session (T2), or the signed-in user or role (T3)")
			case sv != "session" && sv != "user" && sv != "role":
				w.refuse(fl, "server field "+name+" tagged server:"+strconv.Quote(sv), "T2 session is passed in", sessionHint+`; or (T3) User int64 `+"`json:\"user\" server:\"user\"`"+` (or string) and Role string `+"`json:\"role\" server:\"role\"`")
			case sv == "role" && typName != "string":
				w.refuse(fl, "server field "+name+" of type "+typName, ctxRule, userHint)
			case !typed:
				w.refuse(fl, "server field "+name+" of type "+typName, ctxRule, map[bool]string{true: sessionHint, false: userHint}[sv == "session"])
			case structTag(fl.Tag, "path") != "" || structTag(fl.Tag, "query") != "":
				w.refuse(fl, "server field "+name+" with a path or query tag", ctxRule, "A server-set value never comes from the path or the query string; drop the path/query tag")
			case sv != "session" && tag != sv:
				w.refuse(fl, fmt.Sprintf("server:%q field %s with json name %q", sv, name, tag), ctxRule,
					fmt.Sprintf("Its json name is %q, so the English and the HTTP 400 for a request that sends it name it the same: %s", sv, userHint))
			}
			for _, prev := range out {
				if prev.ServerSet == sv {
					w.refuse(fl, "second "+sv+" field "+name, ctxRule, "An Input has at most one server:"+strconv.Quote(sv)+" field")
				}
			}
			server = sv
			switch sv {
			case "session":
				line = fmt.Sprintf(docSentences["field"], tag, strings.NewReplacer("{valid}", value.Valid, "{zero}", value.Zero).Replace(w.env.http.SessionRule))
			default:
				line = "" // T3: filled in finish, once Roles (Public or not) is known
			}
		}
		if server == "" && ctx == "D4 input" {
			switch strings.ToLower(tag) {
			case "user", "role", "user_id", "role_id":
				w.refuse(fl, fmt.Sprintf("field %s with json name %q that the caller sends", name, tag), "T3 user, role passed in", identityHint)
			}
		}
		switch p := structTag(fl.Tag, "path"); {
		case server != "": // T1/T2: the line says how the server sets it
		case p != "":
			line = fmt.Sprintf(docSentences["path field"], tag, p, typ)
		case structTag(fl.Tag, "query") != "":
			line = fmt.Sprintf(docSentences["query field"], tag, typ)
		}
		out = append(out, Field{Name: name, JSON: tag, Type: typ, Domain: dom, Line: line, ServerSet: server, List: list, goType: types.ExprString(fl.Type), pos: w.fset.Position(fl.Pos())})
	}
	if limit && len(out) > MaxInputFields {
		w.refuse(ts, fmt.Sprintf("Input with %d fields", len(out)), "hard limit", fmt.Sprintf("At most %d fields", MaxInputFields))
	}
	return out
}

// fieldType renders a D4/D5 field type; for domain.<T> it also returns T.
// D5 also allows []domain.<T> (one page of rows).
func (w *walker) fieldType(e ast.Expr, ctx string) (string, string) {
	switch e := e.(type) {
	case *ast.Ident:
		if t, ok := typeTemplates[e.Name]; ok {
			return t, ""
		}
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok && x.Name == "domain" {
			return w.domainTypePhrase(e, e.Sel.Name, true), e.Sel.Name
		}
	case *ast.ArrayType:
		if e.Len != nil {
			w.refuse(e, "field type "+types.ExprString(e), ctx, "Use a slice []domain.<T>, not a fixed array")
			return "", ""
		}
		if ctx != "D5 output" {
			w.refuse(e, "field type "+types.ExprString(e), ctx, listHint)
			return "", ""
		}
		sel, ok := e.Elt.(*ast.SelectorExpr)
		if !ok {
			w.refuse(e, "field type "+types.ExprString(e), ctx, "Use []domain.<T>")
			return "", ""
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "domain" {
			w.refuse(e, "field type "+types.ExprString(e), ctx, "Use []domain.<T>")
			return "", ""
		}
		base := w.domainTypePhrase(e, sel.Sel.Name, false)
		list := fmt.Sprintf(typeTemplates["list"], pluralPhrase(base))
		if full := w.domainTypePhrase(e, sel.Sel.Name, true); full != base {
			list += ", each " + strings.TrimPrefix(full, base+", as ")
		}
		return list, sel.Sel.Name
	}
	w.refuse(e, "field type "+types.ExprString(e), ctx, "Use int64, string, bool, domain.<T>, (on Input) a D10 list []int64 or []string, or (on Output) []domain.<T>")
	return "", ""
}

// listType renders a D10 list input: []int64 or []string tagged
// list:"<min>..<max>". The words are runtime/httpx's ListRule (or
// ListRuleExact) and ListElems; the bounds come from the tag, parsed by
// httpx.ParseListTag, the same function Bind uses.
func (w *walker) listType(fl *ast.Field, name string, at *ast.ArrayType, tag string, hasTag bool) (string, string) {
	const ctx = "D10 list input"
	elem := types.ExprString(at.Elt)
	words, ok := w.env.http.ListElems[elem]
	switch {
	case !ok:
		w.refuse(fl, "list field "+name+" of type "+types.ExprString(at), ctx, listHint)
		return "?", ""
	case !hasTag:
		w.refuse(fl, "list field "+name+" without a list tag", ctx, listHint)
		return "?", ""
	case structTag(fl.Tag, "path") != "" || structTag(fl.Tag, "query") != "":
		w.refuse(fl, "list field "+name+" with a path or query tag", ctx, "A list input is sent in the JSON body of a POST, PUT, PATCH or DELETE")
		return "?", ""
	}
	min, max, err := httpx.ParseListTag(tag)
	if err != nil {
		w.refuse(fl, "list field "+name+" whose "+err.Error(), ctx, listHint)
		return "?", ""
	}
	rule := w.env.http.ListRule
	if min == max {
		rule = w.env.http.ListRuleExact
	}
	return strings.NewReplacer("{min}", strconv.Itoa(min), "{max}", strconv.Itoa(max), "{elems}", words).Replace(rule), elem
}

// domainTypePhrase is the `// bridge-en:` display name of a domain type. With full,
// a struct type also lists its JSON fields.
func (w *walker) domainTypePhrase(at ast.Node, name string, full bool) string {
	dt := w.env.domain.Types[name]
	switch {
	case dt == nil:
		w.refuse(at, "domain."+name+", which internal/domain does not declare", "M1 domain type", "")
		return "?"
	case dt.Phrase == "":
		w.refuse(at, "domain type "+name+" without a display name", "M1 domain type",
			fmt.Sprintf("Add `// %s <display name>` to its doc comment (%s)", phrasePrefix, dt.pos))
		return "?"
	case !full || len(dt.Fields) == 0:
		return dt.Phrase
	}
	parts := make([]string, len(dt.Fields))
	for i, f := range dt.Fields {
		typ, ok := typeTemplates[f.Type]
		if inner := w.env.domain.Types[f.Type]; !ok && inner != nil && inner.Phrase != "" {
			typ, ok = inner.Phrase, true
		}
		if !ok || f.JSON == "" {
			w.refuse(at, "domain."+name+" field "+f.Name+" (type "+f.Type+", json "+strconv.Quote(f.JSON)+")", "M1 domain type",
				"Fields of domain structs used in a contract need a json tag and type int64, string, bool or another domain type")
			return "?"
		}
		parts[i] = fmt.Sprintf(structFieldTemplate, f.JSON, typ)
	}
	return fmt.Sprintf(structTypeTemplate, dt.Phrase, joinList(parts))
}

func (w *walker) actionStruct(ts *ast.TypeSpec) {
	st, ok := ts.Type.(*ast.StructType)
	if ok && len(st.Fields.List) == 1 && len(st.Fields.List[0].Names) == 1 &&
		st.Fields.List[0].Names[0].Name == "q" && types.ExprString(st.Fields.List[0].Type) == "*db.Queries" {
		return
	}
	w.refuse(ts, "Action struct of a different shape", "D7 action", "Write exactly: type Action struct { q *db.Queries }")
}

// rolesDecl reads A1: var Roles = httpx.Public, or
// var Roles = httpx.Roles("<role>", ...) with one or more distinct lowercase
// identifiers as string literals. Every role must be one cmd/server
// declares with httpx.AppRoles (A2).
func (w *walker) rolesDecl(vs *ast.ValueSpec) {
	if w.f.rolesSeen {
		w.refuse(vs, "second Roles declaration", "A1 who may call it", "Declare Roles once")
		return
	}
	w.f.rolesSeen = true
	if len(vs.Values) != 1 || vs.Type != nil || !w.pkgs["httpx"] {
		w.refuse(vs, "Roles declaration "+w.print(vs), "A1 who may call it", rolesHint)
		return
	}
	switch v := vs.Values[0].(type) {
	case *ast.SelectorExpr:
		if types.ExprString(v) == "httpx.Public" {
			w.f.Public = true
			return
		}
	case *ast.CallExpr:
		if types.ExprString(v.Fun) != "httpx.Roles" || v.Ellipsis.IsValid() {
			break
		}
		if len(v.Args) == 0 {
			w.refuse(vs, "empty role list httpx.Roles()", "A1 who may call it", "List at least one role, or declare var Roles = httpx.Public for an action anyone may call, signed in or not")
			return
		}
		seen := map[string]bool{}
		for _, a := range v.Args {
			role, ok := stringLit(a)
			switch {
			case !ok:
				w.refuse(a, "role "+types.ExprString(a)+" that is not a string literal", "A1 who may call it", rolesHint)
			case !roleRe.MatchString(role) || len(role) > 32:
				w.refuse(a, fmt.Sprintf("role %q that is not a lowercase identifier", role), "A1 who may call it", "A role name is [a-z][a-z0-9_]*, at most 32 characters, for example \"organizer\" or \"box_office\"")
			case seen[role]:
				w.refuse(a, fmt.Sprintf("role %q listed twice", role), "A1 who may call it", "List each role once")
			case w.env.roles == nil:
				w.refuse(a, fmt.Sprintf("role %q without an app-wide role list", role), "A2 app roles", appRolesHint)
			case !w.env.roles.has[role]:
				w.refuse(a, fmt.Sprintf("role %q that cmd/server does not declare", role), "A2 app roles",
					fmt.Sprintf("The app's roles are declared once, in %s: %s. Use one of them, or add it there", w.env.roles.pos, joinOr(quoteAll(w.env.roles.list))))
			default:
				w.f.Roles = append(w.f.Roles, role)
			}
			seen[role] = true
		}
		return
	}
	w.refuse(vs, "Roles declaration "+w.print(vs), "A1 who may call it", rolesHint)
}

func quoteAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = strconv.Quote(s)
	}
	return out
}

func (w *walker) failureDecl(vs *ast.ValueSpec) {
	name := vs.Names[0].Name
	bad := func() {
		w.refuse(vs, "package-level variable "+name, "D6 failures",
			`The only package-level variables are failure cases: F<n> = failure.New("F<n>", http.Status<Name>, "<message>")`)
	}
	if len(vs.Names) != 1 || len(vs.Values) != 1 || vs.Type != nil || !fidRe.MatchString(name) {
		bad()
		return
	}
	call, ok := vs.Values[0].(*ast.CallExpr)
	if !ok || types.ExprString(call.Fun) != "failure.New" || len(call.Args) != 3 {
		bad()
		return
	}
	id, ok1 := stringLit(call.Args[0])
	msg, ok2 := stringLit(call.Args[2])
	status, ok3 := 0, false
	if sel, ok := call.Args[1].(*ast.SelectorExpr); ok && types.ExprString(sel.X) == "http" {
		status, ok3 = statusCodes[sel.Sel.Name]
	}
	if !ok1 || !ok2 || !ok3 || id != name {
		bad()
		return
	}
	if w.fids[name] != nil {
		w.refuse(vs, "second declaration of "+name, "D6 failures", "F-IDs are unique")
		return
	}
	fc := &FailureCase{ID: name, Message: msg, Status: status, StatusText: http.StatusText(status), pos: w.fset.Position(vs.Pos())}
	w.fids[name] = fc
	w.f.Failures = append(w.f.Failures, fc)
}

func (w *walker) constructor(d *ast.FuncDecl) {
	const want = "func New(q *db.Queries) *Action { return &Action{q: q} }"
	if w.print(&ast.FuncDecl{Name: d.Name, Type: d.Type, Body: d.Body}) != want {
		w.refuse(d, "constructor of a different shape", "D8 constructor", "Write exactly: func New(q *db.Queries) *Action { return &Action{q: q} }")
	}
}

func (w *walker) print(n ast.Node) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, w.fset, n)
	return strings.Join(strings.Fields(buf.String()), " ") // layout-insensitive
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// humanize turns create_invoice into "Create invoice".
func humanize(snake string) string {
	s := strings.ReplaceAll(snake, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// structTag returns the value of key in a struct field tag.
func structTag(tag *ast.BasicLit, key string) string {
	v, _ := structTagOK(tag, key)
	return v
}

// structTagOK is structTag, and whether the tag has key at all.
func structTagOK(tag *ast.BasicLit, key string) (string, bool) {
	if tag == nil {
		return "", false
	}
	s, _ := strconv.Unquote(tag.Value)
	return reflect.StructTag(s).Lookup(key)
}
