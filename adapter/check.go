package adapter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	testFIDRe = regexp.MustCompile(`^Test((?:F[1-9][0-9]*)+)_`)
	oneFIDRe  = regexp.MustCompile(`F[1-9][0-9]*`)
)

// Check is what CI runs per slice. Intent first: intent.md exists and its
// "## Failure cases" section is in the strict format (I1, I2), before
// action.go is read; then action.go renders (refusing unrenderable code) and
// declares exactly the F-IDs intent.md lists (I3). Only then: compare with the
// committed golden .en file and enforce the other cross-checks: every F-ID is
// covered by checks/ (named TestF<n>_ AND referencing <pkg>.F<n>), no dead
// SQL, and the route is served through httpx.Bind with the slice's own
// Roles over txn.DB (whose behaviour the English describes).
func Check(dir string) error {
	intent, err := ParseIntent(dir)
	if err != nil {
		return err
	}
	f, err := ParseAction(dir)
	if err != nil {
		return err
	}
	if err := checkIntentMatches(intent, f); err != nil {
		return err
	}
	got, err := Render(dir)
	if err != nil {
		return err
	}
	var errs []error
	golden := GoldenPath(dir)
	want, err := os.ReadFile(golden)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("%s: golden file missing: run bridge-en -write %s", golden, dir))
	case string(want) != got:
		errs = append(errs, fmt.Errorf("%s: golden file does not match action.go (%s); review the change, then run bridge-en -write %s",
			golden, firstDiff(string(want), got), dir))
	}
	declared := map[string]bool{}
	for _, fc := range f.Failures {
		declared[fc.ID] = true
	}
	errs = append(errs, checkCoverage(dir, f.Package, declared)...)
	used := map[string]bool{}
	for _, c := range f.Queries {
		used[c.Name] = true
	}
	for _, name := range sortedKeys(f.env.queries) {
		if !used[name] {
			errs = append(errs, fmt.Errorf("%s: query %s is never called by action.go (dead SQL)", filepath.Join(dir, f.env.queries[name].File), name))
		}
	}
	if err := checkBound(f.env.root, f.Package); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// checkCoverage: an F-ID is covered by a check named TestF<n>_... whose body
// references the slice's F<n> value (e.g. create_invoice.F1). A name alone
// proves nothing; the reference ties the test to the failure it claims.
func checkCoverage(dir, pkg string, declared map[string]bool) []error {
	files, _ := filepath.Glob(filepath.Join(dir, "checks", "*_test.go"))
	sort.Strings(files)
	covered := map[string]bool{}
	var errs []error
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return []error{err}
		}
		alias := featureImport(file, pkg)
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			m := testFIDRe.FindStringSubmatch(fd.Name.Name)
			if m == nil {
				continue
			}
			refs := map[string]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && alias != "" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == alias {
						refs[sel.Sel.Name] = true
					}
				}
				return true
			})
			for _, id := range oneFIDRe.FindAllString(m[1], -1) {
				switch {
				case !declared[id]:
					errs = append(errs, fmt.Errorf("%s: %s references unknown failure case %s", fset.Position(fd.Pos()), fd.Name.Name, id))
				case !refs[id]:
					errs = append(errs, fmt.Errorf("%s: %s is named for %s but never references %s.%s; assert that failure, not just its name",
						fset.Position(fd.Pos()), fd.Name.Name, id, pkg, id))
				default:
					covered[id] = true
				}
			}
		}
	}
	for _, id := range sortedKeys(declared) {
		if !covered[id] {
			errs = append(errs, fmt.Errorf("%s: no check covers %s (I3 intent = code): add func Test%s_... in checks/ that references %s.%s and runs against real SQLite",
				filepath.Join(dir, "checks"), id, id, pkg, id))
		}
	}
	return errs
}

// featureImport returns the name a file uses for <module>/features/<pkg>.
func featureImport(file *ast.File, pkg string) string {
	for _, is := range file.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		if strings.HasSuffix(path, "/features/"+pkg) {
			if is.Name != nil {
				return is.Name.Name
			}
			return pkg
		}
	}
	return ""
}

// checkBound: cmd/server must serve <pkg>.Route through the runtime's
// httpx.Bind, with the slice's queries built on the runtime's txn.DB, because
// the HTTP and transaction sentences of the English are those of
// runtime/httpx (TxRule needs txn.DB). An app's own package named httpx or
// txn does not count.
func checkBound(root, pkg string) error {
	paths, _ := filepath.Glob(filepath.Join(root, "cmd", "server", "*.go"))
	fset := token.NewFileSet()
	for _, p := range paths {
		file, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		alias := featureImport(file, pkg)
		httpx, txn := importName(file, RuntimePath+"/httpx"), importName(file, RuntimePath+"/txn")
		if alias == "" || httpx == "" || txn == "" {
			continue
		}
		found := false
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 || exprString(call.Args[0]) != alias+".Route" {
				return true
			}
			if bind, ok := call.Args[1].(*ast.CallExpr); ok && exprString(bind.Fun) == httpx+".Bind" &&
				len(bind.Args) == 2 && exprString(bind.Args[0]) == alias+".Roles" { // A3: the slice's own Roles
				ast.Inspect(bind.Args[1], func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok && exprString(c.Fun) == txn+".DB" {
						found = true
					}
					return !found
				})
			}
			return !found
		})
		if found {
			return nil
		}
	}
	return fmt.Errorf("%s: %s.Route is not served with %s/httpx.Bind(%s.Roles, ...) over queries built on %s/txn.DB (H1, A3); the English describes that runtime's behaviour, one transaction per call and who may call the action, so every route goes through Bind with its own Roles: mux.Handle(%s.Route, httpx.Bind(%s.Roles, %s.New(db.New(txn.DB(conn))).Handle))",
		filepath.Join(root, "cmd", "server"), pkg, RuntimePath, pkg, RuntimePath, pkg, pkg, pkg)
}

// importName is the name a file uses for the package at path, or "".
func importName(file *ast.File, path string) string {
	for _, is := range file.Imports {
		if p, _ := strconv.Unquote(is.Path.Value); p == path {
			if is.Name != nil {
				return is.Name.Name
			}
			return path[strings.LastIndex(path, "/")+1:]
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
