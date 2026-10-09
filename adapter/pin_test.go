package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bridge "github.com/pierre10101/go-ai-bridge"
)

func writeGoMod(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestCheckPin: the app's go.mod is the pin. bridge-en -check refuses an app
// that requires another bridge-en version than the binary, or none.
func TestCheckPin(t *testing.T) {
	v := bridge.Version()
	for name, tc := range map[string]struct{ gomod, want string }{
		"one-line require": {"module example.com/app\n\ngo 1.24.0\n\nrequire github.com/pierre10101/go-ai-bridge v" + v + "\n", ""},
		"require block":    {"module example.com/app\n\ngo 1.24.0\n\nrequire (\n\tgithub.com/x/y v1.0.0\n\tgithub.com/pierre10101/go-ai-bridge v" + v + "\n)\n", ""},
		"local replace":    {"module example.com/app\n\nrequire github.com/pierre10101/go-ai-bridge v" + v + "\n\nreplace github.com/pierre10101/go-ai-bridge => ../bridge-en\n", ""},
		"source module":    {"module github.com/pierre10101/go-ai-bridge\n\ngo 1.24.0\n", ""},
		"other version": {"module example.com/app\n\nrequire github.com/pierre10101/go-ai-bridge v9.9.9\n",
			"pins github.com/pierre10101/go-ai-bridge v9.9.9 but this is bridge-en v" + v},
		"no require":   {"module example.com/app\n\ngo 1.24.0\n", "does not require github.com/pierre10101/go-ai-bridge"},
		"similar path": {"module example.com/app\n\nrequire github.com/pierre10101/go-ai-bridge-extra v" + v + "\n", "does not require github.com/pierre10101/go-ai-bridge"},
	} {
		t.Run(name, func(t *testing.T) {
			err := CheckPin(writeGoMod(t, tc.gomod), v)
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// TestFixtureAppPinsThisVersion: the fixture app requires this VERSION, so
// `bridge-en -check adapter/testdata/good/*/` in scripts/ci.sh passes the pin
// check, and a VERSION bump without it fails here first.
func TestFixtureAppPinsThisVersion(t *testing.T) {
	if err := CheckPin("testdata", bridge.Version()); err != nil {
		t.Fatal(err)
	}
}
