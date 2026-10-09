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
//     ListElems, ListWhen and the ErrorBody shape. Bind uses these values and httpx's own tests
//     prove each one, so the HTTP and transaction sentences in every .en file
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
	ErrorShape         string
}

// env is everything outside action.go the adapter reads for one slice.
type env struct {
	root    string
	domain  *domainInfo
	http    *plumbing
	queries map[string]*SQLQuery
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
		BadInput:       outcome(httpx.BadInput),
		Internal:       outcome(httpx.Internal),
		Success:        map[string]int{},
		InputRule:      httpx.InputRule,
		QueryInputRule: httpx.QueryInputRule,
		BadQueryWhen:   httpx.BadQueryWhen,
		TxRule:         httpx.TxRule,
		ReadTxRule:     httpx.ReadTxRule,
		ClockRule:      httpx.ClockRule,
		SessionRule:    httpx.SessionRule,
		SessionValue:   httpx.SessionValue,
		ServerSetWhen:  httpx.ServerSetWhen,
		ListRule:       httpx.ListRule,
		ListRuleExact:  httpx.ListRuleExact,
		ListElems:      httpx.ListElems,
		ListWhen:       httpx.ListWhen,
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
	return &env{root: root, domain: d, http: h, queries: q}, append(domainErrs, refusals...), nil
}
