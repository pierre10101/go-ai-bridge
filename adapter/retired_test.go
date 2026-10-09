package adapter

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// retiredIDs are rule IDs that were renamed and must not come back: the
// strict GET query rule was released for review as "G" + "10" (a family of
// one) and is T4, next to the other request-binding rules T1-T3.
var retiredIDs = []string{"G" + "10"}

// TestNoRetiredRuleIDs: no Grammar pattern uses a retired ID, and no tracked
// file in the repository mentions one as a word, so the RULEBOOK, README,
// AGENTS.md template, English and comments cannot drift back to it.
func TestNoRetiredRuleIDs(t *testing.T) {
	for _, p := range Grammar {
		for _, id := range retiredIDs {
			if p.ID == id {
				t.Errorf("Grammar uses the retired rule ID %s (%s)", id, p.Name)
			}
		}
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	for _, id := range retiredIDs {
		re := regexp.MustCompile(`\b` + id + `\b`)
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if f == "" || strings.HasSuffix(f, ".sum") {
				continue
			}
			src, err := os.ReadFile("../" + f)
			if err != nil {
				continue // deleted in the working tree
			}
			for i, line := range strings.Split(string(src), "\n") {
				if re.MatchString(line) {
					t.Errorf("%s:%d mentions the retired rule ID %s", f, i+1, id)
				}
			}
		}
	}
}
