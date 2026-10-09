package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyModule builds a scratch copy of the fixture app (testdata/go.mod, its
// internal/domain, cmd/server (the app's roles, A2) and good/create_invoice as features/create_invoice: action.go
// + queries), applying edits (path relative to the app root -> rewrite) on the
// way. It returns the slice directory.
func copyModule(t *testing.T, edits map[string]func(string) string) string {
	t.Helper()
	const fixtures = "testdata"
	dst := t.TempDir()
	files := map[string]string{"go.mod": "go.mod", "features/create_invoice/action.go": "good/create_invoice/action.go"}
	for _, pattern := range []string{"internal/domain/*.go", "good/create_invoice/queries/*.sql", "cmd/server/*.go"} {
		m, err := filepath.Glob(filepath.Join(fixtures, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range m {
			if !strings.HasSuffix(p, "_test.go") {
				from, _ := filepath.Rel(fixtures, p)
				files[strings.Replace(from, "good/", "features/", 1)] = from
			}
		}
	}
	for rel, from := range files {
		src, err := os.ReadFile(filepath.Join(fixtures, from))
		if err != nil {
			t.Fatal(err)
		}
		out := string(src)
		if edit := edits[rel]; edit != nil {
			if out = edit(out); out == string(src) {
				t.Fatalf("edit of %s changed nothing", rel)
			}
			delete(edits, rel)
		}
		if err := os.MkdirAll(filepath.Join(dst, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, rel), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, edit := range edits { // new files
		if err := os.WriteFile(filepath.Join(dst, rel), []byte(edit("")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dst, "features", "create_invoice")
}

// TestDomainBodyDrivesCurrencyList: the currency list in the English comes
// from the switch in IsSupportedCurrency, not from a comment. Adding "GBP" to
// the switch, and touching nothing else, changes exactly one line of the
// English: step 2. The doc comment still says nothing about GBP.
func TestDomainBodyDrivesCurrencyList(t *testing.T) {
	const before, after = `case "ZAR", "USD", "EUR":`, `case "ZAR", "USD", "EUR", "GBP":`
	dir := copyModule(t, map[string]func(string) string{
		"internal/domain/money.go": func(s string) string { return strings.Replace(s, before, after, 1) },
	})
	src, _ := os.ReadFile(filepath.Join(dir, "..", "..", "internal", "domain", "money.go"))
	if strings.Count(string(src), "GBP") != 1 {
		t.Fatalf("GBP must appear only in the switch:\n%s", src)
	}
	got, err := Render(dir)
	if err != nil {
		t.Fatalf("unrenderable:\n%v", err)
	}
	golden, err := os.ReadFile(GoldenPath("testdata/good/create_invoice"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(golden), "GBP") {
		t.Fatal("the golden English already mentions GBP")
	}
	want := `2. If the request's ` + "`currency`" + ` is not one of "ZAR", "USD", "EUR" or "GBP", stop with F3: HTTP 422 Unprocessable Entity "currency is not supported".`
	g, w := strings.Split(got, "\n"), strings.Split(string(golden), "\n")
	if len(g) != len(w) {
		t.Fatalf("line count changed: %d -> %d", len(w), len(g))
	}
	var diffs []string
	for i := range g {
		if g[i] != w[i] {
			diffs = append(diffs, g[i])
		}
	}
	if len(diffs) != 1 || diffs[0] != want {
		t.Fatalf("want exactly one changed line:\n  %s\ngot %d:\n  %s", want, len(diffs), strings.Join(diffs, "\n  "))
	}
}

// TestDomainUnderGrammar: internal/domain is refused like action.go when it
// leaves the grammar, or when English is written by hand for a function.
func TestDomainUnderGrammar(t *testing.T) {
	cases := map[string]struct {
		file string
		edit func(string) string
		want string
	}{
		"hand-written function phrase": {"internal/domain/money.go", func(s string) string {
			return strings.Replace(s, "// IsSupportedCurrency reports", "// bridge-en: {code} is a currency we invoice in (ZAR, USD or EUR)\n// IsSupportedCurrency reports", 1)
		}, "internal/domain/money.go:19:1: refused: `bridge-en` comment on function IsSupportedCurrency is not in the allowed pattern list (M2 domain function). English for a domain function is rendered from its body"},
		"description in a display name": {"internal/domain/money.go", func(s string) string {
			return strings.Replace(s, "bridge-en: an amount of money", "bridge-en: an amount of money (whole cents and an ISO 4217 code)", 1)
		}, `internal/domain/money.go:12:6: refused: display name "an amount of money (whole cents and an ISO 4217 code)" of domain type Money with a description`},
		"loop in a domain function": {"internal/domain/invoice_number.go", func(s string) string {
			return strings.Replace(s, `return shape.Has(string(n), "INV-######") && n != "INV-000000"`,
				"for _, c := range string(n) {\n\t\t_ = c\n\t}\n\treturn true", 1)
		}, "internal/domain/invoice_number.go:27:2: refused: range loop is not in the allowed pattern list (domain function IsValidInvoiceNumber)"},
		"panic in a domain function": {"internal/domain/invoice_number.go", func(s string) string {
			return strings.Replace(s, `assert.Pre(seq > 0, "invoice sequence is positive")`, `panic("no")`, 1)
		}, "internal/domain/invoice_number.go:20:2: refused: call to panic as a statement is not in the allowed pattern list (domain function InvoiceNumberFor)"},
		"default clause": {"internal/domain/money.go", func(s string) string {
			return strings.Replace(s, "\t\treturn true\n\t}", "\t\treturn true\n\tdefault:\n\t\treturn false\n\t}", 1)
		}, "internal/domain/money.go:22:2: refused: default clause"},
		"method": {"internal/domain/money.go", func(s string) string {
			return s + "\nfunc (m Money) IsZero() bool { return m.Cents == 0 }\n"
		}, "refused: method IsZero is not in the allowed pattern list (internal/domain)"},
		"arithmetic": {"internal/domain/invoice_number.go", func(s string) string {
			return strings.Replace(s, "seq <= MaxInvoiceSeq", "seq+1 <= MaxInvoiceSeq", 1)
		}, "internal/domain/invoice_number.go:21:13: refused: arithmetic operator + is not in the allowed pattern list (domain function InvoiceNumberFor)"},
		"package-level variable": {"internal/domain/money.go", func(s string) string {
			return s + "\nvar Supported = []string{\"ZAR\"}\n"
		}, "refused: package-level variable Supported is not in the allowed pattern list (internal/domain)"},
		"clock in domain (T1)": {"internal/domain/invoice_number.go", func(s string) string {
			return strings.Replace(s, "\t\"fmt\"\n", "\t\"fmt\"\n\t\"time\"\n", 1) + "\nfunc IsExpired(at int64) bool {\n\treturn at < time.Now().Unix()\n}\n"
		}, `internal/domain/invoice_number.go:5:2: refused: import "time" is not in the allowed pattern list (T1 time is passed in). Logic never reads the clock`},
		"impure import": {"internal/domain/money.go", func(s string) string {
			return strings.Replace(s, "package domain\n", "package domain\n\nimport \"os\"\n\nvar _ = os.Getenv\n", 1)
		}, `refused: import "os" is not in the allowed pattern list (internal/domain). internal/domain is pure: it imports only fmt, github.com/pierre10101/go-ai-bridge/runtime/assert and github.com/pierre10101/go-ai-bridge/runtime/shape`},
		"format verb": {"internal/domain/invoice_number.go", func(s string) string {
			return strings.Replace(s, `"INV-%06d"`, `"INV-%x"`, 1)
		}, `refused: format verb "%x" is not in the allowed pattern list (domain function InvoiceNumberFor)`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := copyModule(t, map[string]func(string) string{tc.file: tc.edit})
			_, err := Render(dir)
			if err == nil {
				t.Fatal("rendered; want refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
}

// TestDomainRendering pins the English of each M3 construct.
func TestDomainRendering(t *testing.T) {
	cases := map[string]string{
		`return shape.Has(string(n), "AB-##-#")`:                       `{n} is AB- followed by two digits followed by - followed by one digit`,
		`return InvoiceNumber(fmt.Sprintf("X%dY%04d", seq, seq)) == n`: `X followed by {seq} followed by Y followed by {seq} as four digits equals {n}`,
		`return !(seq > MaxInvoiceSeq) || n == "INV-000001"`:           `it is false that ({seq} is greater than 999999) or {n} equals the text "INV-000001"`,
		`return IsValidInvoiceNumber(n) && IsSupportedCurrency("ZAR")`: `{n} is INV- followed by six digits and {n} does not equal the text "INV-000000" and the text "ZAR" is one of "ZAR", "USD" or "EUR"`,
	}
	for body, want := range cases {
		t.Run(body, func(t *testing.T) {
			imports := ""
			if strings.Contains(body, "fmt.") {
				imports = "import \"fmt\"\n\n"
			}
			if strings.Contains(body, "shape.") {
				imports = "import \"github.com/pierre10101/go-ai-bridge/runtime/shape\"\n\n"
			}
			src := "package domain\n\n" + imports + "func Probe(n InvoiceNumber, seq int64) bool {\n\t" + body + "\n}\n"
			dir := copyModule(t, map[string]func(string) string{"internal/domain/extra.go": func(string) string { return src }})
			root, err := findModuleRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			info, errs, err := loadDomain(root)
			if err != nil || len(errs) > 0 {
				t.Fatalf("%v %v", err, errs)
			}
			if got := info.Funcs["Probe"].Phrase; got != want {
				t.Fatalf("got  %s\nwant %s", got, want)
			}
		})
	}
}

// TestBoundNeedsTxn: TxRule is only true if the slice's queries are built on
// the runtime's txn.DB, so a route bound with httpx.Bind over a plain *sql.DB
// is refused, and so is a binding through an app's own httpx package, or
// one that does not pass the slice's own Roles (A3).
func TestBoundNeedsTxn(t *testing.T) {
	routes := `package main

import (
	"github.com/pierre10101/go-ai-bridge/features/create_invoice"
	createdb "github.com/pierre10101/go-ai-bridge/features/create_invoice/db"
	"github.com/pierre10101/go-ai-bridge/runtime/httpx"
	"github.com/pierre10101/go-ai-bridge/runtime/txn"
)

func Routes(db any) {
	mux.Handle(create_invoice.Route, httpx.Bind(ROLES, create_invoice.New(createdb.New(DB)).Handle))
}
`
	const refused = "is not served with github.com/pierre10101/go-ai-bridge/runtime/httpx.Bind(create_invoice.Roles, ...) over queries built on github.com/pierre10101/go-ai-bridge/runtime/txn.DB (H1, A3)"
	ownHTTPX := func(s string) string {
		return strings.Replace(s, `"github.com/pierre10101/go-ai-bridge/runtime/httpx"`, `"example.com/app/internal/httpx"`, 1)
	}
	same := func(s string) string { return s }
	// A3: Bind gets the slice's own Roles, not another slice's or a literal.
	otherRoles := func(s string) string { return strings.Replace(s, "ROLES", "create_event.Roles", 1) }
	publicRoles := func(s string) string { return strings.Replace(s, "ROLES", "httpx.Public", 1) }
	for _, tc := range []struct {
		db, want string
		edit     func(string) string
	}{{"txn.DB(db)", "", same}, {"db", refused, same}, {"txn.DB(db)", refused, ownHTTPX},
		{"txn.DB(db)", refused, otherRoles}, {"txn.DB(db)", refused, publicRoles}} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "cmd", "server"), 0o755); err != nil {
			t.Fatal(err)
		}
		src := strings.Replace(tc.edit(strings.Replace(routes, "createdb.New(DB)", "createdb.New("+tc.db+")", 1)), "ROLES", "create_invoice.Roles", 1)
		if err := os.WriteFile(filepath.Join(root, "cmd", "server", "routes.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		err := checkBound(root, "create_invoice")
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", tc.db, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: want %q, got %v", tc.db, tc.want, err)
		}
	}
}
