// Package initdocs holds the agent instructions `bridge-en init` writes into
// an app: AGENTS.md, the single source of truth (the cross-tool standard), and
// thin pointer files for common agents that say "follow AGENTS.md" and give
// the five rules that matter most. The templates are embedded in the binary,
// so `go install` users get them. init writes documents only: never Go code,
// never runtime or adapter source, and never over an existing file unless
// forced.
package initdocs

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templates embed.FS

// Doc is one file init writes: its path in the app and its template.
type Doc struct {
	Path     string // slash-separated, relative to the app root
	Template string // file in templates/
}

// Docs are the files init writes, AGENTS.md first.
var Docs = []Doc{
	{"AGENTS.md", "AGENTS.md"},
	{".cursor/rules/bridge-en.mdc", "cursor-rule.mdc"},
	{"CLAUDE.md", "CLAUDE.md"},
	{".github/copilot-instructions.md", "copilot-instructions.md"},
	{"GEMINI.md", "GEMINI.md"},
}

// Content renders the document at path for bridge-en version.
func Content(d Doc, version string) (string, error) {
	src, err := templates.ReadFile("templates/" + d.Template)
	if err != nil {
		return "", err
	}
	rules, err := templates.ReadFile("templates/pointer-rules.md")
	if err != nil {
		return "", err
	}
	s := strings.ReplaceAll(string(src), "{{RULES}}", strings.TrimRight(string(rules), "\n"))
	return strings.ReplaceAll(s, "{{VERSION}}", version), nil
}

// Result is what init did with one document.
type Result struct {
	Path   string
	Action string // "wrote", "overwrote" or "kept"
}

// Write writes every document under root. An existing file is kept (and
// reported) unless force is set. root must be an existing directory.
func Write(root, version string, force bool) ([]Result, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	var out []Result
	for _, d := range Docs {
		content, err := Content(d, version)
		if err != nil {
			return out, err
		}
		path := filepath.Join(root, filepath.FromSlash(d.Path))
		action := "wrote"
		if fi, err := os.Lstat(path); err == nil {
			if !force {
				out = append(out, Result{d.Path, "kept"})
				continue
			}
			if !fi.Mode().IsRegular() {
				return out, fmt.Errorf("%s exists and is not a regular file; not overwriting it", path)
			}
			action = "overwrote"
		} else if !errors.Is(err, fs.ErrNotExist) {
			return out, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return out, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return out, err
		}
		out = append(out, Result{d.Path, action})
	}
	return out, nil
}
