package prcomment

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a git repository with a base commit and returns its directory
// and a function that commits the working tree.
func repo(t *testing.T, files map[string]string) (string, func(msg string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	write(t, dir, files)
	commit := func(msg string) string {
		git("add", "-A")
		git("commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	return dir, commit
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		if c == "" {
			os.Remove(full)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func inDir(t *testing.T, dir string) {
	t.Helper()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
}

const intentA = "# Intent: A\n\n## Failure cases\n- F1: the thing is missing.\n"

func TestComment(t *testing.T) {
	dir, commit := repo(t, map[string]string{
		"features/a/intent.md": intentA,
		"features/a/a.en":      "# A\n\n- F1 \"missing\": HTTP 404.\n",
		"features/a/action.go": "package a\n",
		"features/b/intent.md": "# Intent: B\n\n## Failure cases\nNone.\n",
		"features/b/b.en":      "# B\n",
		"features/b/action.go": "package b\n",
		"features/c/intent.md": "# Intent: C\n\n## Failure cases\nNone.\n",
		"features/c/c.en":      "# C\n",
	})
	base := commit("base")
	write(t, dir, map[string]string{
		// a: intent and English change together
		"features/a/intent.md": intentA + "- F2: the thing is ```locked```.\n",
		"features/a/a.en":      "# A\n\n- F1 \"missing\": HTTP 404.\n- F2 \"locked\": HTTP 409.\n",
		// b: code changes, English does not
		"features/b/action.go": "package b // changed\n",
		// d: a new feature
		"features/d/intent.md": "# Intent: D\n\n## Failure cases\nNone.\n",
		"features/d/d.en":      "# D\n",
	})
	commit("change")
	inDir(t, dir)
	opt := Options{Base: base, Dirs: []string{"features/a/", "features/b", "features/c", "features/d"}, Version: "9.9.9"}
	got, err := Build(opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		Marker + "\n",
		"3 feature" + "s changed between",
		"### `features/a`",
		"**Intent** (`intent.md`, changed in this pull request (diff)):",
		"+- F2: the thing is ```locked```.",
		"````diff", // fence longer than the content's backticks
		"**English** (`a.en`, diff):",
		"+- F2 \"locked\": HTTP 409.",
		"### `features/b`",
		"Changed: `action.go`",
		"**Intent** (`intent.md`, unchanged):",
		"**English** (`b.en`): unchanged.",
		"### `features/d`",
		"**Intent** (`intent.md`, new in this pull request):",
		"`bridge-en pr-comment` 9.9.9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "features/c") {
		t.Errorf("unchanged feature c is listed:\n%s", got)
	}
	if strings.Contains(got, "diff --git") || strings.Contains(got, "\nindex ") {
		t.Errorf("git header lines leaked:\n%s", got)
	}
	// Idempotent: the same input gives the same comment.
	again, _ := Build(opt)
	if again != got {
		t.Error("two runs differ")
	}
}

func TestNoChange(t *testing.T) {
	dir, commit := repo(t, map[string]string{"features/a/intent.md": intentA})
	base := commit("base")
	inDir(t, dir)
	got, err := Build(Options{Base: base, Dirs: []string{"features/a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, Marker+"\n") || !strings.Contains(got, "No feature changed") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestMissingIntentAndEnglish(t *testing.T) {
	dir, commit := repo(t, map[string]string{"README": "x\n"})
	base := commit("base")
	write(t, dir, map[string]string{"features/a/action.go": "package a\n"})
	commit("code without intent")
	inDir(t, dir)
	got, err := Build(Options{Base: base, Dirs: []string{"features/a"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"no `intent.md`. `bridge-en -check` refuses this feature (I1)", "(`a.en`): missing. Run `bridge-en -write features/a`"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestSizeLimit: a huge diff is shortened, never past MaxChars, and the
// comment says so; small features stay whole.
func TestSizeLimit(t *testing.T) {
	big := strings.Repeat("- a line of English that goes on for a while.\n", 3000)
	dir, commit := repo(t, map[string]string{
		"features/a/intent.md": intentA, "features/a/a.en": "# A\n",
		"features/b/intent.md": intentA, "features/b/b.en": "# B\n",
	})
	base := commit("base")
	write(t, dir, map[string]string{"features/a/a.en": "# A\n" + big, "features/b/b.en": "# B\n- one more line\n"})
	commit("big")
	inDir(t, dir)
	for _, max := range []int{5000, 20000, DefaultMaxChars} {
		got, err := Build(Options{Base: base, Dirs: []string{"features/a", "features/b"}, MaxChars: max})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) > max {
			t.Fatalf("max %d: comment has %d chars", max, len(got))
		}
		for _, want := range []string{"more lines not shown: size limit", "shortened to keep this comment under the size limit", "+- one more line"} {
			if !strings.Contains(got, want) {
				t.Errorf("max %d: missing %q", max, want)
			}
		}
	}
	// Very many features: still under the limit, the rest named.
	var dirs []string
	files := map[string]string{}
	for i := 0; i < 200; i++ {
		d := "features/f" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26))
		dirs = append(dirs, d)
		files[d+"/intent.md"] = intentA
	}
	dir2, commit2 := repo(t, map[string]string{"README": "x\n"})
	base2 := commit2("base")
	write(t, dir2, files)
	commit2("many")
	inDir(t, dir2)
	got, err := Build(Options{Base: base2, Dirs: dirs, MaxChars: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 3000 {
		t.Fatalf("comment has %d chars", len(got))
	}
	if !strings.Contains(got, "size limit") {
		t.Fatalf("no size-limit note:\n%s", got)
	}
}

func TestBadRevision(t *testing.T) {
	if _, err := Build(Options{Base: "--output=/tmp/x", Dirs: []string{"features/a"}}); err == nil {
		t.Fatal("an option-like revision was accepted")
	}
	if _, err := Build(Options{Dirs: []string{"features/a"}}); err == nil {
		t.Fatal("no base was accepted")
	}
}
