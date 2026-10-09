package adapter

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	bridge "github.com/pierre10101/go-ai-bridge"
	"github.com/pierre10101/go-ai-bridge/internal/initdocs"
)

// TestAgentsSkeletonRenders: the example feature in the AGENTS.md that
// `bridge-en init` writes is inside the grammar: its intent.md is in the I2
// format and names the F-IDs its action.go declares, and the action renders.
func TestAgentsSkeletonRenders(t *testing.T) {
	agents, err := initdocs.Content(initdocs.Docs[0], bridge.Version())
	if err != nil {
		t.Fatal(err)
	}
	block := func(label string) string {
		re := regexp.MustCompile("(?s)`" + regexp.QuoteMeta(label) + "`:\n\n```[a-z]*\n(.*?)\n```\n")
		m := re.FindStringSubmatch(agents)
		if m == nil {
			t.Fatalf("AGENTS.md has no block for %s", label)
		}
		return m[1] + "\n"
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                       "module example.com/app\n\ngo 1.24.0\n\nrequire " + SourceModule + " v" + bridge.Version() + "\n",
		"schema.sql":                   "CREATE TABLE seats (id INTEGER PRIMARY KEY, held_by TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0);\n",
		"features/hold_seat/intent.md": block("intent.md"),
		"features/hold_seat/queries/claim_seat.sql": block("queries/claim_seat.sql"),
		"features/hold_seat/action.go":              block("action.go"),
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "features", "hold_seat")
	if err := CheckIntentFirst(dir); err != nil {
		t.Fatalf("AGENTS.md skeleton fails intent checks:\n%v", err)
	}
	en, err := Render(dir)
	if err != nil {
		t.Fatalf("AGENTS.md skeleton is refused:\n%v", err)
	}
	// The caller is the session the server sets, never a body field, and the
	// hold ends at expires_at (boundary: expires_at == now has expired).
	for _, w := range []string{
		"The request body is one JSON object with this field and no others:\n- `seat_id`: a whole number.\n",
		"- `session`: set by the server from the session cookie",
		"1. If the session from the cookie equals the text \"\", stop with F2",
		"set `held_by` = the session from the cookie and `expires_at` = 10 minutes after the current time",
		"(`held_by` is the text \"\" or `expires_at` is no later than the current time) at that moment",
		"stop with F1",
		"Who may call it: anyone, signed in or not.",
	} {
		if !strings.Contains(en, w) {
			t.Errorf("English lacks %q:\n%s", w, en)
		}
	}
	for _, bad := range []string{"held_at", "PersonID", "minutes or more before"} {
		if strings.Contains(agents, bad) {
			t.Errorf("AGENTS.md still teaches %q", bad)
		}
	}
	for _, bad := range []string{"person_id", "user_id", "held_at", "the request's `session`"} {
		if strings.Contains(en, bad) {
			t.Errorf("English still says %q:\n%s", bad, en)
		}
	}
}
