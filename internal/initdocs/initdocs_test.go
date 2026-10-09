package initdocs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritesDocsOnly(t *testing.T) {
	root := t.TempDir()
	res, err := Write(root, "9.9.9", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(Docs) {
		t.Fatalf("results %v", res)
	}
	var written []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			written = append(written, filepath.ToSlash(rel))
			if strings.HasSuffix(p, ".go") || !(strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdc")) {
				t.Errorf("init wrote a non-document %s", rel)
			}
		}
		return nil
	})
	want := ".cursor/rules/bridge-en.mdc .github/copilot-instructions.md AGENTS.md CLAUDE.md GEMINI.md"
	if got := strings.Join(written, " "); got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
	for _, d := range Docs {
		b, _ := os.ReadFile(filepath.Join(root, d.Path))
		s := string(b)
		if strings.Contains(s, "{{") {
			t.Errorf("%s has an unfilled placeholder", d.Path)
		}
		if d.Path == "AGENTS.md" {
			continue
		}
		for _, w := range []string{"Follow AGENTS.md", "bridge-en 9.9.9", "1. Write `features/<slice>/intent.md` FIRST", "5. Change code only through pull requests"} {
			if !strings.Contains(s, w) {
				t.Errorf("%s: missing %q", d.Path, w)
			}
		}
	}
	mdc, _ := os.ReadFile(filepath.Join(root, ".cursor/rules/bridge-en.mdc"))
	if !strings.HasPrefix(string(mdc), "---\ndescription: ") || !strings.Contains(string(mdc), "alwaysApply: true\n---\n") {
		t.Errorf("cursor rule has no front matter:\n%s", mdc)
	}
}

func TestAgentsCoversTheWorkflow(t *testing.T) {
	s, err := Content(Docs[0], "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"go get github.com/pierre10101/go-ai-bridge@v9.9.9",
		"go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v9.9.9",
		"intent.md` FIRST", "## Failure cases", "- F1: ",
		"sqlc generate", "real SQLite",
		"bridge-en -check features/<slice>/     # after EVERY edit", "A refusal is an instruction",
		"bridge-en -write", "read `<slice>.en` against `intent.md`",
		"Never edit a `.en` file by hand",
		`clock:"now"`, "Session string `json:\"session\" server:\"session\"`", "Never call `time.Now()`",
		"User int64 `json:\"user\" server:\"user\"`", "Role string `json:\"role\" server:\"role\"`",
		"var Roles = httpx.Public", `var Roles = httpx.Roles("organizer", "admin")`, "httpx.AppRoles",
		"httpx.Bind(<slice>.Roles, ", "httpx.Identify(AppRoles, identity, mux)", "password hashing",
		"never from the request body", `if in.Session == ""`, "expires_at <= sqlc.arg(now)",
		"one conditional UPDATE", "if n != 1", "int64(len(in.IDs))",
		"Keep the UI thin", "expires_at - now", "error.id",
		"Never push to main", "RULEBOOK.md", "bridge-en -grammar",
		"blob/v9.9.9/RULEBOOK.md",
		"-- owner: organizer_id", "AND\n  organizer_id = sqlc.arg(organizer_id)", "`OrganizerID: in.User`",
		`.BypassOwnership("admin")`, "only events you own", "Public action never writes it",
		"A GET takes only the query values it declares (T4)", "answered with HTTP 400",
		"-- owner: event_id -> events.organizer_id", "Child rows prove their parent is yours (A5)",
		"SELECT events.id,\n  sqlc.arg(name) FROM events", "sections.event_id IN (SELECT events.id FROM events",
		"only sections of events you\n  own", "Q0-Q10", "A1-A5",
		"Deletes name one row by its key (Q10)", "`ON DELETE CASCADE`",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("AGENTS.md: missing %q", w)
		}
	}
}

func TestNeverOverwritesWithoutForce(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "CLAUDE.md")
	os.WriteFile(mine, []byte("my own notes\n"), 0o644)
	res, err := Write(root, "9.9.9", false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "my own notes\n" {
		t.Fatal("CLAUDE.md was overwritten without -force")
	}
	actions := map[string]string{}
	for _, r := range res {
		actions[r.Path] = r.Action
	}
	if actions["CLAUDE.md"] != "kept" || actions["AGENTS.md"] != "wrote" {
		t.Fatalf("results %v", res)
	}
	// A second run keeps everything.
	res, _ = Write(root, "9.9.9", false)
	for _, r := range res {
		if r.Action != "kept" {
			t.Fatalf("second run %v", res)
		}
	}
	res, err = Write(root, "9.9.10", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Action != "overwrote" {
			t.Fatalf("-force %v", res)
		}
	}
	if b, _ := os.ReadFile(mine); !strings.Contains(string(b), "bridge-en 9.9.10") {
		t.Fatal("-force did not overwrite")
	}
}

func TestRefusesMissingDir(t *testing.T) {
	if _, err := Write(filepath.Join(t.TempDir(), "nope"), "9.9.9", false); err == nil {
		t.Fatal("init into a missing directory succeeded")
	}
}
