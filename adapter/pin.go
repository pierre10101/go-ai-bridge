package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// An app never copies bridge-en source. It imports the runtime packages
// (RuntimePath + "/httpx" and so on) and pins them like any Go dependency:
// the bridge-en version in its go.mod. The English quotes that runtime, so
// bridge-en -check refuses an app whose go.mod pins another version than the
// installed binary (CheckPin).

// SourceModule is the module bridge-en and its runtime are released from.
const SourceModule = "github.com/pierre10101/go-ai-bridge"

// RuntimePath is the import path prefix of the runtime packages
// (assert, failure, httpx, page, shape, store, txn).
const RuntimePath = SourceModule + "/runtime"

// ModuleRoot is the directory holding the go.mod above dir.
func ModuleRoot(dir string) (string, error) { return findModuleRoot(dir) }

var (
	moduleRe     = regexp.MustCompile(`(?m)^module\s+"?([^"\s]+)"?\s*(//.*)?$`)
	requireOneRe = regexp.MustCompile(`(?m)^require\s+"?` + regexp.QuoteMeta(SourceModule) + `"?\s+(\S+)`)
	requireBlkRe = regexp.MustCompile(`(?ms)^require\s*\((.*?)^\)`)
	requireLnRe  = regexp.MustCompile(`(?m)^\s*"?` + regexp.QuoteMeta(SourceModule) + `"?\s+(\S+)`)
)

// PinnedVersion reads root/go.mod and returns the app's module path and the
// version of SourceModule it requires ("" when it requires none).
func PinnedVersion(root string) (module, version string, err error) {
	path := filepath.Join(root, "go.mod")
	src, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	m := moduleRe.FindSubmatch(src)
	if m == nil {
		return "", "", fmt.Errorf("%s: no module line", path)
	}
	module = string(m[1])
	if r := requireOneRe.FindSubmatch(src); r != nil {
		return module, string(r[1]), nil
	}
	for _, blk := range requireBlkRe.FindAllSubmatch(src, -1) {
		if r := requireLnRe.FindSubmatch(blk[1]); r != nil {
			return module, string(r[1]), nil
		}
	}
	return module, "", nil
}

// CheckPin refuses an app (the module at root) whose go.mod does not require
// bridge-en at exactly this binary's version: the English this binary writes
// quotes the runtime of its own version. The source module itself passes.
func CheckPin(root, version string) error {
	module, got, err := PinnedVersion(root)
	if err != nil || module == SourceModule {
		return err
	}
	want := "v" + strings.TrimPrefix(version, "v")
	path := filepath.Join(root, "go.mod")
	switch got {
	case want:
		return nil
	case "":
		return fmt.Errorf("%s: does not require %s; the English quotes its runtime, so pin it: go get %s@%s", path, SourceModule, SourceModule, want)
	}
	return fmt.Errorf("%s: pins %s %s but this is bridge-en %s; install the pinned version (go install %s/cmd/bridge-en@%s) or move the app (go get %s@%s), then review every .en diff",
		path, SourceModule, got, want, SourceModule, got, SourceModule, want)
}
