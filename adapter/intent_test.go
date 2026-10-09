package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntentRefusals: each testdata/bad_intent/<case> (a renderable
// create_invoice action with a broken or missing intent.md) is refused by
// -check with exactly its want.err (I1, I2, I3).
func TestIntentRefusals(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/bad_intent/*")
	if len(dirs) < 6 {
		t.Fatalf("want at least 6 bad_intent cases, have %d", len(dirs))
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			if _, err := Render(dir); err != nil {
				t.Fatalf("the action itself must render; only intent.md is broken:\n%v", err)
			}
			err := Check(dir)
			if err == nil {
				t.Fatal("-check passed a slice whose intent.md is broken")
			}
			if werr := CheckIntentFirst(dir); werr == nil || werr.Error() != err.Error() {
				t.Fatalf("-write must refuse exactly as -check:\n%v\nvs\n%v", werr, err)
			}
			got := err.Error() + "\n"
			wantPath := filepath.Join(dir, "want.err")
			if *update {
				if err := os.WriteFile(wantPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, rerr := os.ReadFile(wantPath)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if string(want) != got {
				t.Fatalf("refusal changed\n--- want\n%s--- got\n%s", want, got)
			}
		})
	}
}

// TestIntentFormat: what the I2 parser accepts and refuses.
func TestIntentFormat(t *testing.T) {
	cases := []struct {
		name, src string
		ids       []string
		want      string // substring of the refusal; "" = accepted
	}{
		{"plain", "# Intent\n\n## Failure cases\n- F1: a.\n- F2: b.\n", []string{"F1", "F2"}, ""},
		{"F-ID list item in another section", "## Failure cases\n- F1: a.\n\n## Out of scope\n- F9 later.\n", nil, `failure case F9 outside "## Failure cases"`},
		{"continuation", "## Failure cases\n- F1: a long case\n  that goes on.\n\n- F3: c.\n\n## Out of scope\nNothing.\n", []string{"F1", "F3"}, ""},
		{"crlf", "## Failure cases\r\n- F1: a.\r\n", []string{"F1"}, ""},
		{"none", "## Failure cases\nNone.\n", []string{}, ""},
		{"entry after none", "## Failure cases\nNone.\n- F1: a.\n", nil, `line "- F1: a." after "None."`},
		{"empty section", "## Failure cases\n\n## Out of scope\n", nil, `empty "## Failure cases" section`},
		{"no space after colon", "## Failure cases\n- F1:a.\n", nil, `line "- F1:a."`},
		{"no text", "## Failure cases\n- F1:\n", nil, `line "- F1:"`},
		{"F0", "## Failure cases\n- F0: a.\n", nil, `line "- F0: a."`},
		{"star bullet", "## Failure cases\n* F1: a.\n", nil, `line "* F1: a."`},
		{"indented first line", "## Failure cases\n  - F1: a.\n", nil, `line "  - F1: a."`},
		{"heading case", "## Failure Cases\n- F1: a.\n", nil, `heading "## Failure Cases"`},
		{"two headings", "## Failure cases\n- F1: a.\n## Failure cases\n- F2: b.\n", nil, `second "## Failure cases" heading (the first is on line 1)`},
		{"duplicate", "## Failure cases\n- F1: a.\n- F1: b.\n", nil, "second failure case F1 (the first is on line 2)"},
		{"order", "## Failure cases\n- F2: a.\n- F1: b.\n", nil, "failure case F1 after F2"},
		{"bold outside", "Intro\n- **F1** — a.\n## Failure cases\n- F1: a.\n", nil, `failure case F1 outside "## Failure cases"`},
		{"prose mentioning F1 outside is fine", "We number cases from F1 up.\n## Failure cases\n- F1: a.\n", []string{"F1"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "intent.md"), []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			in, err := ParseIntent(dir)
			if c.want == "" {
				if err != nil {
					t.Fatalf("refused:\n%v", err)
				}
				if got := strings.Join(in.IDs(), ","); got != strings.Join(c.ids, ",") {
					t.Fatalf("ids %q, want %q", got, strings.Join(c.ids, ","))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want refusal containing %q, got %v", c.want, err)
			}
			if !strings.Contains(err.Error(), `"- F<n>: <text>"`) {
				t.Fatalf("refusal does not show the expected format: %v", err)
			}
		})
	}
}

// TestGoodIntentsAreStrict: every good fixture's intent.md is in the I2
// format and lists exactly the F-IDs its action.go declares.
func TestGoodIntentsAreStrict(t *testing.T) {
	for _, dir := range goodDirs(t) {
		if err := CheckIntentFirst(dir); err != nil {
			t.Errorf("%s:\n%v", dir, err)
		}
	}
}
