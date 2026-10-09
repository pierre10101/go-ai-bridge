package adapter

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// intent.md is written before any code (I1). bridge-en reads one part of it,
// strictly (I2): the section "## Failure cases", one line per failure case,
//
//	## Failure cases
//	- F1: the customer does not exist.
//	- F2: the amount is zero or negative,
//	  continued on lines indented by two spaces.
//
// and refuses a slice whose F-IDs there differ from the F-IDs action.go
// declares (I3). The rest of intent.md (why, who, inputs, outputs, out of
// scope) is free English for the reviewer.

// IntentHeading is the one heading of intent.md that bridge-en parses.
const IntentHeading = "## Failure cases"

// IntentFormat is quoted in every I1/I2 refusal.
const IntentFormat = `Under the heading "` + IntentHeading + `", write one line per failure case, "- F<n>: <text>" (for example "- F1: the customer does not exist."), F-IDs in increasing order, each once; a long case continues on lines indented by two spaces; a slice with no failure case writes the single line "None."`

const intentFormatShort = `Expected "- F<n>: <text>" (the format is in the first refusal above)`

var (
	intentEntryRe  = regexp.MustCompile(`^- (F[1-9][0-9]*): (\S.*)$`)
	intentLooseRe  = regexp.MustCompile(`^\s*[-*+]?\s*[*_]*\s*(F[0-9]+)\b`)
	intentHeadRe   = regexp.MustCompile(`^#{1,6}\s`)
	intentFailHead = regexp.MustCompile(`(?i)^#{1,6}\s*failure cases\s*$`)
)

// IntentCase is one "- F<n>: <text>" entry of intent.md.
type IntentCase struct {
	ID, Text string
	Line     int
}

// Intent is the parsed "## Failure cases" section of a slice's intent.md.
type Intent struct {
	Path  string
	Cases []IntentCase
}

// IDs lists the F-IDs of the section, in file order.
func (in *Intent) IDs() []string {
	ids := make([]string, len(in.Cases))
	for i, c := range in.Cases {
		ids[i] = c.ID
	}
	return ids
}

func intentRefusal(path string, line int, construct, rule, hint string) Refusal {
	pos := token.Position{Filename: path}
	if line > 0 {
		pos.Line, pos.Column = line, 1
	}
	return Refusal{Pos: pos, Construct: construct, Context: rule, Hint: hint}
}

// ParseIntent reads dir/intent.md and refuses a missing file (I1) or a
// "## Failure cases" section that is not in the strict format (I2).
func ParseIntent(dir string) (*Intent, error) {
	path := filepath.Join(dir, "intent.md")
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Refusals{intentRefusal(path, 0, "feature without intent.md", "I1 intent first",
			"Write intent.md before any code: why, inputs, outputs and every failure case. "+IntentFormat)}
	}
	if err != nil {
		return nil, err
	}
	in := &Intent{Path: path}
	var rs Refusals
	const i2 = "I2 failure cases"
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	heading, inSection, none, prevEntry := 0, false, false, false
	seen := map[string]int{}
	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimRight(line, " \t")
		if intentHeadRe.MatchString(trimmed) {
			inSection = false
			switch {
			case trimmed == IntentHeading && heading == 0:
				heading, inSection, prevEntry = n, true, false
			case trimmed == IntentHeading:
				rs = append(rs, intentRefusal(path, n, fmt.Sprintf("second %q heading (the first is on line %d)", IntentHeading, heading), i2, IntentFormat))
			case intentFailHead.MatchString(trimmed):
				rs = append(rs, intentRefusal(path, n, fmt.Sprintf("heading %q", trimmed), i2, `The heading is exactly "`+IntentHeading+`". `+IntentFormat))
			}
			continue
		}
		if !inSection {
			if m := intentLooseRe.FindStringSubmatch(line); m != nil && strings.TrimSpace(line) != "" && looksLikeListItem(line) {
				rs = append(rs, intentRefusal(path, n, fmt.Sprintf("failure case %s outside %q", m[1], IntentHeading), i2, IntentFormat))
			}
			continue
		}
		switch {
		case trimmed == "":
			// blank lines are allowed between entries
		case trimmed == "None." && len(in.Cases) == 0 && !none:
			none, prevEntry = true, false
		case strings.HasPrefix(line, "  ") && prevEntry:
			// continuation of the previous entry
		default:
			m := intentEntryRe.FindStringSubmatch(trimmed)
			if m == nil || none {
				what := fmt.Sprintf("line %q in %q", clip(trimmed), IntentHeading)
				if none {
					what = fmt.Sprintf("line %q after \"None.\"", clip(trimmed))
				}
				rs = append(rs, intentRefusal(path, n, what, i2, IntentFormat))
				prevEntry = false
				continue
			}
			id := m[1]
			if first, ok := seen[id]; ok {
				rs = append(rs, intentRefusal(path, n, fmt.Sprintf("second failure case %s (the first is on line %d)", id, first), i2, IntentFormat))
			} else if k := len(in.Cases); k > 0 && fidNum(id) < fidNum(in.Cases[k-1].ID) {
				rs = append(rs, intentRefusal(path, n, fmt.Sprintf("failure case %s after %s", id, in.Cases[k-1].ID), i2, IntentFormat))
			}
			seen[id] = n
			in.Cases = append(in.Cases, IntentCase{ID: id, Text: m[2], Line: n})
			prevEntry = true
		}
	}
	if heading == 0 {
		rs = append(rs, intentRefusal(path, 0, fmt.Sprintf("intent.md without the heading %q", IntentHeading), i2, IntentFormat))
	} else if len(in.Cases) == 0 && !none && len(rs) == 0 {
		rs = append(rs, intentRefusal(path, heading, fmt.Sprintf("empty %q section", IntentHeading), i2, IntentFormat))
	}
	if len(rs) > 0 {
		// The full format once, on the first refusal; the others point to it.
		sortRefusals(rs)
		for i := 1; i < len(rs); i++ {
			rs[i].Hint = intentFormatShort
		}
		return nil, rs
	}
	return in, nil
}

// looksLikeListItem: a markdown list item ("- ", "* ", "+ ") or a line that
// starts with the F-ID itself ("F1: ...", "**F1** ...").
func looksLikeListItem(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") ||
		strings.HasPrefix(t, "F") || strings.HasPrefix(t, "**F") || strings.HasPrefix(t, "__F")
}

func clip(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return s
}

// checkIntentMatches refuses a slice whose intent.md and action.go name
// different F-IDs (I3), naming every ID missing on either side.
func checkIntentMatches(in *Intent, f *Feature) error {
	const i3 = "I3 intent = code"
	listed := map[string]int{}
	for _, c := range in.Cases {
		listed[c.ID] = c.Line
	}
	declared := map[string]*FailureCase{}
	for _, fc := range f.Failures {
		declared[fc.ID] = fc
	}
	var missing, extra []string
	for _, fc := range f.Failures {
		if _, ok := listed[fc.ID]; !ok {
			missing = append(missing, fc.ID)
		}
	}
	for _, c := range in.Cases {
		if declared[c.ID] == nil {
			extra = append(extra, c.ID)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	summary := fmt.Sprintf("intent.md lists %s; action.go declares %s", idList(in.IDs()), idList(sortedFIDs(declared)))
	var rs Refusals
	for _, id := range missing {
		rs = append(rs, Refusal{Pos: declared[id].pos, Construct: fmt.Sprintf("failure case %s that intent.md does not list", id), Context: i3,
			Hint: fmt.Sprintf("%s. Write the case in intent.md first (\"- %s: <text>\" under %q), or remove %s from action.go", summary, id, IntentHeading, id)})
	}
	for _, id := range extra {
		rs = append(rs, intentRefusal(in.Path, listed[id], fmt.Sprintf("failure case %s that action.go does not declare", id), i3,
			fmt.Sprintf("%s. Declare it in action.go (%s = failure.New(%q, http.Status<Name>, \"<message>\")) and raise it with a guard, or remove it from intent.md", summary, id, id)))
	}
	return rs
}

func sortedFIDs[V any](m map[string]V) []string {
	ids := sortedKeys(m)
	sortFIDs(ids)
	return ids
}

func sortFIDs(ids []string) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && fidNum(ids[j]) < fidNum(ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func idList(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// CheckIntentFirst is what -write runs before it writes the English: the
// slice has an intent.md in the strict format (I1, I2) whose F-IDs are those
// of action.go (I3).
func CheckIntentFirst(dir string) error {
	in, err := ParseIntent(dir)
	if err != nil {
		return err
	}
	f, err := ParseAction(dir)
	if err != nil {
		return err
	}
	return checkIntentMatches(in, f)
}
