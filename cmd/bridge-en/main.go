// Command bridge-en renders feature slices to controlled English.
//
//	bridge-en features/create_invoice          print the English
//	bridge-en -write features/create_invoice   update the golden .en file (after the go.mod pin check)
//	bridge-en -check features/*/               CI: check the go.mod pin, refuse, diff golden, cross-check
//	bridge-en -grammar                         print the allowed pattern list
//	bridge-en -version                         print the version
//
// Install the version an app pins in its go.mod (see RULEBOOK.md "Install and pin"):
//
//	go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v<version>
package main

import (
	"flag"
	"fmt"
	"os"

	bridge "github.com/pierre10101/go-ai-bridge"
	"github.com/pierre10101/go-ai-bridge/adapter"
)

func main() {
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
		fmt.Fprintln(os.Stderr, "usage: bridge-en [-check|-write] <feature-dir>... | -grammar | -version")
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
