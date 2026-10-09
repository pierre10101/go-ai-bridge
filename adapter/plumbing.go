package adapter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
)

// The adapter never invents behaviour it cannot read. Two places outside
// action.go feed the English:
//
//   - the app's internal/domain (domain.go), read with go/ast: every function
//     is under the grammar (M2, M3) and its English is rendered from its
//     body; a type's only hand-written English is its
//     `// bridge-en: <display name>` line (M1).
//   - runtime/httpx, which the app imports and this binary is built with:
//     the declared outcomes BadInput and Internal, the SuccessStatus table,
//     InputRule, QueryInputRule, BadQueryWhen, TxRule, ReadTxRule, ClockRule,
//     SessionRule, SessionValue, ServerSetWhen, ListRule, ListRuleExact,
//     ListElems, ListWhen, StrictQueryRule (T4) and the ErrorBody shape.
//     Bind uses these values and httpx's own tests prove each one, so the HTTP and transaction sentences in every .en file
//     are tied to the plumbing.

// outcome is an httpx.Outcome as the English uses it.
type outcome struct {
	Status                  int
	ID, Message, When, Note string
}

type plumbing struct {
	BadInput, Internal outcome
	Success            map[string]int // route method -> status
	InputRule          string
	QueryInputRule     string
	BadQueryWhen       string
	TxRule             string // {first}, {last} and {commit} are step numbers
	ReadTxRule         string // GET: {first}, {last} and {end} are step numbers
	ClockRule          string // T1: how a `clock:"now"` Input field is filled
	SessionRule        string // T2: how a `server:"session"` Input field is filled; {valid} and {zero} per type
	SessionValue       map[string]struct{ Valid, Zero string }
	ServerSetWhen      string // the BadInput condition of an action with a server-set input
	ListRule           string // D10: a list input; {min}, {max} and {elems}
	ListRuleExact      string // D10: a list input whose min equals its max
	ListElems          map[string]string
	ListWhen           string // the BadInput condition of an action with a list input
	PublicRule         string // A1: who may call a Public action
	RolesRule          string // A1, A3: who may call an action declared with Roles; {roles}
	Unauthenticated    outcome
	Forbidden          outcome
	UserRule           string // T3: how a `server:"user"` Input field is filled; {signed out}
	RoleRule           string // T3: how a `server:"role"` Input field is filled; {signed out}
	SignedOutRule      string // T3, Public actions: {zero}
	SignedOutZero      map[string]string
	ErrorShape         string
	StrictQueryRule    string // T4: a GET's query string takes only the listed values
}

// appRoles is the app-wide role list (A2): the one httpx.AppRoles(...) call
// in cmd/server.
type appRoles struct {
	list   []string
	has    map[string]bool
	pos    token.Position
	bypass []string        // A4: the roles of .BypassOwnership(...) chained on the call, in declared order
	passes map[string]bool // A4: the same, as a set
}

// env is everything outside action.go the adapter reads for one slice.
type env struct {
	root    string
	domain  *domainInfo
	http    *plumbing
	queries map[string]*SQLQuery
	roles   *appRoles         // nil when cmd/server declares no httpx.AppRoles
	owners  map[string]*owner // A4: owned tables (schema.sql "-- owner: <col>"), by table
}

// findModuleRoot walks up from dir to the directory holding go.mod.
func findModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("%s: no go.mod above this directory; bridge-en reads the app's go.mod and internal/domain from the module root", dir)
		}
	}
}

// parsePkg parses the non-test Go files of root/rel, naming files relative to root.
func parsePkg(fset *token.FileSet, root, rel string) ([]*ast.File, error) {
	paths, err := filepath.Glob(filepath.Join(root, rel, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, filepath.Join(rel, filepath.Base(p)), src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no Go files (bridge-en reads this package)", filepath.Join(root, rel))
	}
	return files, nil
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func jsonTag(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	raw, _ := strconv.Unquote(tag.Value)
	name, _, _ := strings.Cut(reflect.StructTag(raw).Get("json"), ",")
	if name == "-" {
		return ""
	}
	return name
}

// loadHTTPX takes the HTTP and transaction sentences from the runtime this
// binary is built with (runtime/httpx of the same bridge-en version). The app
// imports that runtime at the version its go.mod pins, and CheckPin refuses
// any other version, so the quoted sentences are the ones Bind follows.
func loadHTTPX() (*plumbing, error) {
	p := &plumbing{
		BadInput:        outcome(httpx.BadInput),
		Internal:        outcome(httpx.Internal),
		Success:         map[string]int{},
		InputRule:       httpx.InputRule,
		QueryInputRule:  httpx.QueryInputRule,
		BadQueryWhen:    httpx.BadQueryWhen,
		TxRule:          httpx.TxRule,
		ReadTxRule:      httpx.ReadTxRule,
		ClockRule:       httpx.ClockRule,
		SessionRule:     httpx.SessionRule,
		SessionValue:    httpx.SessionValue,
		ServerSetWhen:   httpx.ServerSetWhen,
		ListRule:        httpx.ListRule,
		ListRuleExact:   httpx.ListRuleExact,
		ListElems:       httpx.ListElems,
		ListWhen:        httpx.ListWhen,
		PublicRule:      httpx.PublicRule,
		RolesRule:       httpx.RolesRule,
		Unauthenticated: outcome(httpx.Unauthenticated),
		Forbidden:       outcome(httpx.Forbidden),
		UserRule:        httpx.UserRule,
		RoleRule:        httpx.RoleRule,
		SignedOutRule:   httpx.SignedOutRule,
		SignedOutZero:   httpx.SignedOutZero,
		StrictQueryRule: httpx.StrictQueryRule,
	}
	for m, st := range httpx.SuccessStatus {
		p.Success[m] = st
	}
	for _, ph := range []string{"{first}", "{last}", "{commit}"} {
		if !strings.Contains(p.TxRule, ph) {
			return nil, fmt.Errorf("%s/httpx: TxRule must mention %s", RuntimePath, ph)
		}
	}
	for _, ph := range []string{"{valid}", "{zero}"} {
		if !strings.Contains(p.SessionRule, ph) {
			return nil, fmt.Errorf("%s/httpx: SessionRule must mention %s", RuntimePath, ph)
		}
	}
	for _, ph := range []string{"{min}", "{max}", "{elems}"} {
		if !strings.Contains(p.ListRule, ph) || (ph != "{max}" && !strings.Contains(p.ListRuleExact, ph)) {
			return nil, fmt.Errorf("%s/httpx: ListRule and ListRuleExact must mention %s", RuntimePath, ph)
		}
	}
	if !strings.Contains(p.RolesRule, "{roles}") || !strings.Contains(p.SignedOutRule, "{zero}") ||
		!strings.Contains(p.UserRule, "{signed out}") || !strings.Contains(p.RoleRule, "{signed out}") {
		return nil, fmt.Errorf("%s/httpx: RolesRule must mention {roles}, SignedOutRule {zero}, UserRule and RoleRule {signed out}", RuntimePath)
	}
	shape, err := errorShape(reflect.TypeOf(httpx.ErrorBody{}))
	if err != nil {
		return nil, fmt.Errorf("%s/httpx: %v", RuntimePath, err)
	}
	p.ErrorShape = shape
	return p, nil
}

// errorShape renders ErrorBody{Error ErrorDetail{ID, Message}} from its json tags.
func errorShape(body reflect.Type) (string, error) {
	bad := fmt.Errorf("bridge-en needs type ErrorBody struct { Error ErrorDetail } and type ErrorDetail struct { ID, Message string }, all with json tags")
	if body.Kind() != reflect.Struct || body.NumField() != 1 {
		return "", bad
	}
	outer := body.Field(0)
	detail := outer.Type
	if detail.Kind() != reflect.Struct || detail.NumField() != 2 {
		return "", bad
	}
	tag := func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		return name
	}
	tags := map[string]string{}
	for i := 0; i < detail.NumField(); i++ {
		tags[detail.Field(i).Name] = tag(detail.Field(i))
	}
	if tag(outer) == "" || tags["ID"] == "" || tags["Message"] == "" {
		return "", bad
	}
	return fmt.Sprintf(`{"%s": {"%s": <id>, "%s": <message>}}`, tag(outer), tags["ID"], tags["Message"]), nil
}

func loadEnv(dir string) (*env, Refusals, error) {
	root, err := findModuleRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	d, domainErrs, err := loadDomain(root)
	if err != nil {
		return nil, nil, err
	}
	h, err := loadHTTPX()
	if err != nil {
		return nil, nil, err
	}
	q, refusals, err := LoadQueries(filepath.Join(dir, "queries"))
	if err != nil {
		return nil, nil, err
	}
	keyErrs, err := checkClaimKeys(root, q)
	if err != nil {
		return nil, nil, err
	}
	refusals = append(refusals, keyErrs...)
	delErrs, err := checkDeletes(root, q) // Q10: key and ON DELETE against schema.sql
	if err != nil {
		return nil, nil, err
	}
	refusals = append(refusals, delErrs...)
	roles, roleErrs, err := loadAppRoles(root)
	if err != nil {
		return nil, nil, err
	}
	refusals = append(refusals, roleErrs...)
	owners, ownerErrs, err := loadOwners(root)
	if err != nil {
		return nil, nil, err
	}
	refusals = append(refusals, ownerErrs...)
	refusals = append(refusals, checkProofs(owners, q)...) // A5: Q8, Q9 against schema.sql
	return &env{root: root, domain: d, http: h, queries: q, roles: roles, owners: owners}, append(domainErrs, refusals...), nil
}

// loadAppRoles reads A2: the app's roles, declared once in cmd/server as
// httpx.AppRoles("<role>", ...) with the runtime's httpx, every role a
// distinct lowercase identifier given as a string literal. No declaration
// is not an error here (an app whose actions are all Public needs none);
// an action that lists a role then is refused.
func loadAppRoles(root string) (*appRoles, Refusals, error) {
	paths, _ := filepath.Glob(filepath.Join(root, "cmd", "server", "*.go"))
	sort.Strings(paths)
	fset := token.NewFileSet()
	var found *appRoles
	var bypassCall *ast.CallExpr // A4: <AppRoles call>.BypassOwnership(...)
	var errs Refusals
	refuse := func(n ast.Node, construct, hint string) {
		errs = append(errs, Refusal{Pos: fset.Position(n.Pos()), Construct: construct, Context: "A2 app roles", Hint: hint})
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		rel, _ := filepath.Rel(root, p)
		file, err := parser.ParseFile(fset, filepath.ToSlash(rel), src, 0)
		if err != nil {
			return nil, nil, err
		}
		hx := importName(file, RuntimePath+"/httpx")
		if hx == "" {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "BypassOwnership" {
				inner, ok := sel.X.(*ast.CallExpr)
				switch {
				case !ok || exprString(inner.Fun) != hx+".AppRoles":
					errs = append(errs, Refusal{Pos: fset.Position(call.Pos()), Construct: exprString(call.Fun) + " that is not chained on the httpx.AppRoles call",
						Context: "A4 ownership", Hint: bypassHint})
				case bypassCall != nil:
					errs = append(errs, Refusal{Pos: fset.Position(call.Pos()), Construct: "second BypassOwnership call (the first is at " + fset.Position(bypassCall.Pos()).String() + ")",
						Context: "A4 ownership", Hint: bypassHint})
				default:
					bypassCall = call
				}
				return true
			}
			if exprString(call.Fun) != hx+".AppRoles" {
				return true
			}
			if found != nil {
				refuse(call, "second httpx.AppRoles call (the first is at "+found.pos.String()+")", "Declare the app's roles once")
				return true
			}
			found = &appRoles{has: map[string]bool{}, pos: fset.Position(call.Pos())}
			if len(call.Args) == 0 || call.Ellipsis.IsValid() {
				refuse(call, "httpx.AppRoles without roles", appRolesHint)
			}
			for _, a := range call.Args {
				role, ok := stringLit(a)
				switch {
				case !ok:
					refuse(a, "app role "+exprString(a)+" that is not a string literal", appRolesHint)
				case !roleRe.MatchString(role) || len(role) > 32:
					refuse(a, fmt.Sprintf("app role %q that is not a lowercase identifier", role), "A role name is [a-z][a-z0-9_]*, at most 32 characters")
				case found.has[role]:
					refuse(a, fmt.Sprintf("app role %q listed twice", role), "List each role once")
				default:
					found.has[role] = true
					found.list = append(found.list, role)
				}
			}
			return true
		})
	}
	if bypassCall != nil && found != nil {
		found.passes = map[string]bool{}
		if len(bypassCall.Args) == 0 || bypassCall.Ellipsis.IsValid() {
			errs = append(errs, Refusal{Pos: fset.Position(bypassCall.Pos()), Construct: "BypassOwnership without roles", Context: "A4 ownership", Hint: bypassHint})
		}
		for _, a := range bypassCall.Args {
			role, ok := stringLit(a)
			bad := func(construct string) {
				errs = append(errs, Refusal{Pos: fset.Position(a.Pos()), Construct: construct, Context: "A4 ownership", Hint: bypassHint})
			}
			switch {
			case !ok:
				bad("ownership-bypass role " + exprString(a) + " that is not a string literal")
			case !found.has[role]:
				bad(fmt.Sprintf("ownership-bypass role %q that httpx.AppRoles does not declare", role))
			case found.passes[role]:
				bad(fmt.Sprintf("ownership-bypass role %q listed twice", role))
			default:
				found.passes[role] = true
				found.bypass = append(found.bypass, role)
			}
		}
	}
	return found, errs, nil
}

// bypassHint is attached to refusals of the ownership-bypass declaration.
const bypassHint = "Mark the roles that bypass ownership (A4) once, chained on the app's role list in cmd/server: var AppRoles = httpx.AppRoles(\"customer\", \"organizer\", \"admin\").BypassOwnership(\"admin\"); each one a role that list declares, given as a string literal"
