package adapter

import (
	"os"
	"regexp"
	"testing"
)

// TestRulebookCoversGrammar: RULEBOOK.md has one "### <ID>" section per
// Grammar pattern and no section for a pattern bridge-en does not have, so
// the prompt and the parser contract cannot drift apart.
func TestRulebookCoversGrammar(t *testing.T) {
	src, err := os.ReadFile("../RULEBOOK.md")
	if err != nil {
		t.Fatal(err)
	}
	inBook := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^### ([A-Z][0-9]+)$`).FindAllStringSubmatch(string(src), -1) {
		if inBook[m[1]] {
			t.Errorf("RULEBOOK.md has two sections %s", m[1])
		}
		inBook[m[1]] = true
	}
	inGrammar := map[string]bool{}
	for _, p := range Grammar {
		inGrammar[p.ID] = true
		if !inBook[p.ID] {
			t.Errorf("RULEBOOK.md has no section ### %s (%s)", p.ID, p.Name)
		}
	}
	for id := range inBook {
		if !inGrammar[id] {
			t.Errorf("RULEBOOK.md documents %s, which is not in the grammar", id)
		}
	}
}
