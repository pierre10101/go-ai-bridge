// Command bridge-en renders feature slices to controlled English.
//
//	bridge-en features/create_invoice          print the English
//	bridge-en -write features/create_invoice   update the golden .en file (after the go.mod pin check)
//	bridge-en -check features/*/               CI: check the go.mod pin, intent.md (I1-I3), refuse, diff golden, cross-check
//	bridge-en -grammar                         print the allowed pattern list
//	bridge-en -version                         print the version
//	bridge-en init [-force] [dir]              write AGENTS.md and agent pointer files (docs only)
//	bridge-en pr-comment -base <rev> <feature-dir>...   print the PR comment: intent next to the .en diff
//
// Install the version an app pins in its go.mod (see RULEBOOK.md "Install and pin"):
//
//	go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v<version>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	bridge "github.com/pierre10101/go-ai-bridge"
	"github.com/pierre10101/go-ai-bridge/adapter"
	"github.com/pierre10101/go-ai-bridge/internal/initdocs"
	"github.com/pierre10101/go-ai-bridge/internal/prcomment"
)

const usage = "usage: bridge-en [-check|-write] <feature-dir>... | -grammar | -version | init [-force] [dir] | pr-comment -base <rev> [-head <rev>] [-max <chars>] <feature-dir>..."

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			os.Exit(runInit(os.Args[2:]))
		case "pr-comment":
			os.Exit(runPRComment(os.Args[2:]))
		}
	}
	check := flag.Bool("check", false, "check that the app's go.mod pins this version, render, compare with golden .en and run rulebook cross-checks")
	write := flag.Bool("write", false, "check that the app's go.mod pins this version, then write the golden .en file")
	grammar := flag.Bool("grammar", false, "print the allowed pattern list and exit")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	switch {
	case *version:
		fmt.Println("bridge-en " + bridge.Version())
		return
	case *grammar:
		fmt.Print(adapter.GrammarText())
		return
	}
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	// The English quotes this binary's runtime, so it is only the app's
	// English if the app's go.mod pins this version. -check and -write refuse
	// otherwise; printing warns.
	pinned := map[string]bool{}
	skew := false
	for _, dir := range flag.Args() {
		root, err := adapter.ModuleRoot(dir)
		if err == nil && !pinned[root] {
			pinned[root] = true
			err = adapter.CheckPin(root, bridge.Version())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			skew = true
		}
	}
	if skew && (*check || *write) {
		os.Exit(1)
	}
	failed := false
	for _, dir := range flag.Args() {
		if err := run(dir, *check, *write); err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		} else if *check {
			fmt.Printf("ok  %s\n", dir)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func run(dir string, check, write bool) error {
	if check {
		return adapter.Check(dir)
	}
	if write {
		// Intent first: no English is saved for a slice whose intent.md is
		// missing, malformed or names other F-IDs than action.go (I1-I3).
		if err := adapter.CheckIntentFirst(dir); err != nil {
			return err
		}
	}
	out, err := adapter.Render(dir)
	if err != nil {
		return err
	}
	if write {
		return os.WriteFile(adapter.GoldenPath(dir), []byte(out), 0o644)
	}
	fmt.Print(out)
	return nil
}

// runInit writes the agent docs into a directory: AGENTS.md and pointer
// files. Documents only; an existing file is kept unless -force.
func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite existing files")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bridge-en init [-force] [dir]\n\nWrites AGENTS.md (the workflow for AI agents) and pointer files for Cursor, Claude, Copilot and Gemini into dir (default .). Documents only, never code. Existing files are kept unless -force.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 2
	}
	root := "."
	if fs.NArg() == 1 {
		root = fs.Arg(0)
	}
	results, err := initdocs.Write(root, bridge.Version(), *force)
	kept := 0
	for _, r := range results {
		if r.Action == "kept" {
			kept++
			fmt.Printf("kept      %s (exists; bridge-en init -force overwrites it)\n", r.Path)
		} else {
			fmt.Printf("%-9s %s\n", r.Action, r.Path)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bridge-en init:", err)
		return 1
	}
	if kept == 0 {
		fmt.Println("Next: read AGENTS.md. Every agent pointer file says: follow AGENTS.md.")
	}
	return 0
}

// runPRComment prints the markdown of the pull request comment: each changed
// feature's intent.md next to its .en diff. The pr-english action posts it.
func runPRComment(args []string) int {
	fs := flag.NewFlagSet("pr-comment", flag.ContinueOnError)
	base := fs.String("base", "", "the pull request's base commit (required)")
	head := fs.String("head", "HEAD", "the pull request's head commit")
	max := fs.Int("max", prcomment.DefaultMaxChars, "the most characters the comment may have")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: bridge-en pr-comment -base <rev> [-head <rev>] [-max <chars>] <feature-dir>...\n\nPrints a markdown comment: for each feature directory changed between base and head, its intent.md (in full, or its diff when changed) next to the diff of its .en file. Starts with "+prcomment.Marker+".")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *base == "" || fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	wd, _ := os.Getwd()
	dirs := make([]string, 0, fs.NArg())
	for _, d := range fs.Args() {
		if filepath.IsAbs(d) && wd != "" {
			if rel, err := filepath.Rel(wd, d); err == nil {
				d = rel
			}
		}
		dirs = append(dirs, d)
	}
	out, err := prcomment.Build(prcomment.Options{Base: *base, Head: *head, Dirs: dirs, MaxChars: *max, Version: bridge.Version()})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(out)
	return 0
}
