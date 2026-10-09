package adapter

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

var doc = template.Must(template.New("doc").Parse(docTemplate))

// Render compiles one slice directory to controlled English.
// It returns Refusals when anything cannot be rendered.
func Render(dir string) (string, error) {
	f, err := ParseAction(dir)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := doc.Execute(&buf, f); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func fidNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "F"))
	return n
}

// GoldenPath is where a slice's committed English lives: <dir>/<name>.en.
func GoldenPath(dir string) string {
	return filepath.Join(dir, filepath.Base(filepath.Clean(dir))+".en")
}

// firstDiff reports the first differing line between two renderings.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  golden: %s\n  actual: %s", i+1, a, b)
		}
	}
	return "files differ"
}
