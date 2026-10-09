// Package prcomment builds the pull request comment of `bridge-en
// pr-comment`: for every feature a pull request changes, its intent.md (in
// full, or its diff when the pull request changes it) next to the diff of its
// golden English (<slice>.en). A reviewer reads what was asked next to what
// the code does. The comment starts with Marker, so a workflow updates its
// one comment instead of adding another, and it never exceeds MaxChars.
//
// It only reads git: `git diff <base>...<head>` and `git show <rev>:<path>`.
package prcomment

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Marker is the first line of every comment; the pr-english action finds the
// comment to update by it.
const Marker = "<!-- bridge-en:pr-english -->"

// DefaultMaxChars keeps the comment under GitHub's 65536-character limit for
// an issue comment, with room to spare.
const DefaultMaxChars = 60000

// minMaxChars is the smallest limit that still holds the header and a
// one-line note per feature.
const minMaxChars = 1000

// Options says what to compare.
type Options struct {
	Base     string   // the pull request's base commit (required)
	Head     string   // default HEAD
	Dirs     []string // feature directories, e.g. features/*/
	MaxChars int      // default DefaultMaxChars
	Version  string   // bridge-en version, quoted in the footer
	// Git runs git with args in the current directory and returns stdout.
	// Default: the git binary.
	Git func(args ...string) (string, error)
}

// Feature is what the comment says about one changed feature directory.
type Feature struct {
	Dir          string
	Files        []string // changed files, relative to Dir
	IntentState  string   // "new", "changed", "unchanged", "missing", "deleted"
	Intent       string   // full intent.md, or its diff when IntentState is "changed"
	EnglishName  string   // <slice>.en
	EnglishDiff  string   // "" when the English did not change
	EnglishState string   // "new", "changed", "unchanged", "missing", "deleted"
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

var revRe = regexp.MustCompile(`^[A-Za-z0-9_./@^~-]+$`)

// Collect returns the changed features among opt.Dirs, in the order given.
func Collect(opt Options) ([]Feature, error) {
	if opt.Base == "" {
		return nil, errors.New("pr-comment: -base is required (the pull request's base commit, e.g. github.event.pull_request.base.sha)")
	}
	if opt.Head == "" {
		opt.Head = "HEAD"
	}
	for _, rev := range []string{opt.Base, opt.Head} {
		if !revRe.MatchString(rev) || strings.HasPrefix(rev, "-") {
			return nil, fmt.Errorf("pr-comment: %q is not a git revision", rev)
		}
	}
	git := opt.Git
	if git == nil {
		git = runGit
	}
	rng := opt.Base + "..." + opt.Head
	var feats []Feature
	seen := map[string]bool{}
	for _, d := range opt.Dirs {
		dir := filepath.ToSlash(filepath.Clean(d))
		if seen[dir] {
			continue
		}
		seen[dir] = true
		names, err := git("diff", "--no-ext-diff", "--name-only", "--relative", rng, "--", dir)
		if err != nil {
			return nil, err
		}
		var files []string
		for _, n := range strings.Split(strings.TrimSpace(names), "\n") {
			if n = strings.TrimSpace(n); n != "" {
				rel := strings.TrimPrefix(n, dir+"/")
				files = append(files, rel)
			}
		}
		if len(files) == 0 {
			continue
		}
		f := Feature{Dir: dir, Files: files, EnglishName: filepath.Base(dir) + ".en"}
		f.IntentState, f.Intent, err = fileState(git, opt.Base, opt.Head, rng, dir+"/intent.md", true)
		if err != nil {
			return nil, err
		}
		f.EnglishState, f.EnglishDiff, err = fileState(git, opt.Base, opt.Head, rng, dir+"/"+f.EnglishName, false)
		if err != nil {
			return nil, err
		}
		feats = append(feats, f)
	}
	return feats, nil
}

// fileState says whether path is new, changed, unchanged, missing or deleted
// between base and head, and returns its diff (changed, new or deleted) or,
// when full is set and it did not change, its content at head.
func fileState(git func(...string) (string, error), base, head, rng, path string, full bool) (string, string, error) {
	_, errHead := git("cat-file", "-e", head+":./"+path)
	_, errBase := git("cat-file", "-e", base+":./"+path)
	inHead, inBase := errHead == nil, errBase == nil
	diff, err := git("diff", "--no-ext-diff", "--no-color", "--unified=3", "--relative", rng, "--", path)
	if err != nil {
		return "", "", err
	}
	diff = trimDiffHeader(diff)
	switch {
	case !inHead && !inBase:
		return "missing", "", nil
	case !inHead:
		return "deleted", diff, nil
	case !inBase:
		if full {
			content, err := git("show", head+":./"+path)
			return "new", content, err
		}
		return "new", diff, nil
	case diff != "":
		return "changed", diff, nil
	case full:
		content, err := git("show", head+":./"+path)
		return "unchanged", content, err
	}
	return "unchanged", "", nil
}

// trimDiffHeader drops the "diff --git" and "index" lines; the ---/+++ lines
// and hunks stay.
func trimDiffHeader(d string) string {
	var keep []string
	for _, l := range strings.Split(d, "\n") {
		if strings.HasPrefix(l, "diff --git ") || strings.HasPrefix(l, "index ") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n")
}

// Build collects the changed features and renders the comment.
func Build(opt Options) (string, error) {
	feats, err := Collect(opt)
	if err != nil {
		return "", err
	}
	return Render(opt, feats), nil
}

func short(rev string) string {
	if len(rev) > 12 && regexp.MustCompile(`^[0-9a-f]+$`).MatchString(rev) {
		return rev[:7]
	}
	return rev
}

// Render writes the markdown comment, at most opt.MaxChars characters.
func Render(opt Options, feats []Feature) string {
	max := opt.MaxChars
	if max <= 0 {
		max = DefaultMaxChars
	}
	if max < minMaxChars {
		max = minMaxChars
	}
	head := opt.Head
	if head == "" {
		head = "HEAD"
	}
	var b strings.Builder
	b.WriteString(Marker + "\n")
	b.WriteString("## bridge-en: intent and English\n\n")
	if len(feats) == 0 {
		fmt.Fprintf(&b, "No feature changed between `%s` and `%s`.\n", short(opt.Base), short(head))
		b.WriteString(footer(opt, false))
		return b.String()
	}
	plural := "s"
	if len(feats) == 1 {
		plural = ""
	}
	fmt.Fprintf(&b, "%d feature%s changed between `%s` and `%s`. For each, read the **intent** (what was asked, written first) next to the **English** (what the code does, generated by bridge-en; never edited by hand). Every failure case `F<n>` in the intent must be in the English with the same boundary; anything in the English that the intent does not ask for is a question for the author.\n",
		len(feats), plural, short(opt.Base), short(head))
	foot := footer(opt, true)
	remaining := max - b.Len() - len(foot)
	truncated := false
	// Share the room fairly: a feature that fits in its share is shown in
	// full and leaves the rest of its share to the others.
	var dropped []string
	fulls := make([]string, len(feats))
	budgets := make([]int, len(feats))
	open := map[int]bool{}
	for i, f := range feats {
		fulls[i], _ = section(f, 1<<30)
		open[i] = true
	}
	for len(open) > 0 {
		share := remaining / len(open)
		fitted := false
		for i := range feats {
			if open[i] && len(fulls[i]) <= share {
				budgets[i] = len(fulls[i])
				remaining -= len(fulls[i])
				delete(open, i)
				fitted = true
			}
		}
		if !fitted {
			for i := range feats {
				if open[i] {
					budgets[i] = share
				}
			}
			break
		}
	}
	for i, f := range feats {
		sec, cut := section(f, budgets[i])
		if len(sec) > budgets[i] {
			sec = omitted(f)
			cut = true
			if len(sec) > budgets[i] {
				sec = ""
				dropped = append(dropped, f.Dir)
			}
		}
		truncated = truncated || cut
		b.WriteString(sec)
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&b, "\nNot shown (size limit): %s.\n", codeList(dropped))
	}
	if truncated {
		b.WriteString(foot)
	} else {
		b.WriteString(footer(opt, false))
	}
	out := b.String()
	if len(out) > max { // only with very many features: never post more than the limit
		out = strings.ToValidUTF8(out[:max], "")
	}
	return out
}

func footer(opt Options, truncated bool) string {
	var b strings.Builder
	b.WriteString("\n---\n")
	if truncated {
		b.WriteString("Some parts were shortened to keep this comment under the size limit; the full files are under **Files changed**.\n")
	}
	v := opt.Version
	if v == "" {
		v = "dev"
	}
	fmt.Fprintf(&b, "<sub>`bridge-en pr-comment` %s: updated in place on every push. The English is checked by `bridge-en -check`; the intent is the author's.</sub>\n", v)
	return b.String()
}

func omitted(f Feature) string {
	return fmt.Sprintf("\n### `%s`\n\nNot shown: this comment reached its size limit. See `%s/intent.md` and `%s/%s` under **Files changed**.\n", f.Dir, f.Dir, f.Dir, f.EnglishName)
}

var intentLabel = map[string]string{
	"new":       "new in this pull request",
	"changed":   "changed in this pull request (diff)",
	"unchanged": "unchanged",
	"deleted":   "deleted in this pull request",
}

var englishLabel = map[string]string{
	"new":     "new in this pull request",
	"changed": "diff",
	"deleted": "deleted in this pull request",
}

// section renders one feature within budget characters, shortening the two
// blocks if it has to. It reports whether it shortened anything.
func section(f Feature, budget int) (string, bool) {
	var intro strings.Builder
	fmt.Fprintf(&intro, "\n### `%s`\n\nChanged: %s\n", f.Dir, codeList(f.Files))
	intentHead, intentBody, intentLang := "", "", "markdown"
	switch f.IntentState {
	case "missing":
		intentHead = "\n**Intent**: no `intent.md`. `bridge-en -check` refuses this feature (I1): the intent is written first.\n"
	default:
		intentHead = fmt.Sprintf("\n**Intent** (`intent.md`, %s):\n\n", intentLabel[f.IntentState])
		intentBody = f.Intent
		if f.IntentState == "changed" || f.IntentState == "deleted" {
			intentLang = "diff"
		}
	}
	enHead, enBody := "", ""
	switch {
	case f.EnglishState == "missing":
		enHead = fmt.Sprintf("\n**English** (`%s`): missing. Run `bridge-en -write %s` and commit it.\n", f.EnglishName, f.Dir)
	case f.EnglishDiff == "":
		enHead = fmt.Sprintf("\n**English** (`%s`): unchanged. The code changed, but what it does, in the English, did not.\n", f.EnglishName)
	default:
		enHead = fmt.Sprintf("\n**English** (`%s`, %s):\n\n", f.EnglishName, englishLabel[f.EnglishState])
		enBody = f.EnglishDiff
	}
	render := func(ib, eb string) string {
		var s strings.Builder
		s.WriteString(intro.String())
		s.WriteString(intentHead)
		if f.IntentState != "missing" {
			s.WriteString(fence(intentLang, ib))
		}
		s.WriteString(enHead)
		if enBody != "" {
			s.WriteString(fence("diff", eb))
		}
		return s.String()
	}
	full := render(intentBody, enBody)
	if len(full) <= budget {
		return full, false
	}
	// Shorten: give each block a share of what the skeleton leaves.
	skeleton := len(render("", "")) + 2*200
	room := budget - skeleton
	if room <= 0 {
		return full, true // caller replaces it with the omitted note
	}
	ib, eb := intentBody, enBody
	if len(ib)+len(eb) > room {
		share := room / 2
		switch {
		case len(ib) <= share:
			eb = cut(eb, room-len(ib))
		case len(eb) <= share:
			ib = cut(ib, room-len(eb))
		default:
			ib, eb = cut(ib, share), cut(eb, share)
		}
	}
	return render(ib, eb), true
}

// cut keeps whole lines of s within n characters and says how many it left out.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var b strings.Builder
	kept := 0
	for _, l := range lines {
		if b.Len()+len(l)+1 > n-80 {
			break
		}
		b.WriteString(l + "\n")
		kept++
	}
	fmt.Fprintf(&b, "... (%d more lines not shown: size limit)\n", len(lines)-kept)
	return b.String()
}

// fence wraps s in a code block whose fence is longer than any run of
// backticks inside s, so the content cannot close it.
func fence(lang, s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest >= n {
		n = longest + 1
	}
	f := strings.Repeat("`", n)
	return f + lang + "\n" + strings.TrimRight(s, "\n") + "\n" + f + "\n"
}

func codeList(files []string) string {
	q := make([]string, len(files))
	for i, f := range files {
		q[i] = "`" + f + "`"
	}
	return strings.Join(q, ", ")
}
