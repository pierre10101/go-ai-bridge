package adapter

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// checkASCII refuses non-ASCII bytes in schema.sql and the slice's
// queries/*.sql. sqlc's SQLite parser can mis-tokenize a multi-byte character
// in a SQL comment and report a misleading error dozens of lines away; the
// English renderer is ASCII anyway.
func checkASCII(root, queriesDir string) (Refusals, error) {
	var errs Refusals
	paths := []string{filepath.Join(root, "schema.sql")}
	qfiles, err := filepath.Glob(filepath.Join(queriesDir, "*.sql"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, qfiles...)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) && filepath.Base(path) == "schema.sql" {
				continue
			}
			return nil, err
		}
		if r := firstNonASCII(path, src); r != nil {
			errs = append(errs, *r)
		}
	}
	return errs, nil
}

func firstNonASCII(path string, src []byte) *Refusal {
	line, col := 1, 1
	for i := 0; i < len(src); {
		b := src[i]
		if b < 0x80 {
			if b == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(src[i:])
		hint := fmt.Sprintf("bridge-en's English and sqlc's SQLite parser are ASCII; replace U+%04X with an ASCII character (for example a hyphen instead of an em dash)", r)
		if r == utf8.RuneError && size == 1 {
			hint = fmt.Sprintf("invalid UTF-8 byte 0x%02X; bridge-en refuses non-ASCII in schema.sql and queries/*.sql", b)
		}
		return &Refusal{
			Pos:       token.Position{Filename: filepath.ToSlash(path), Line: line, Column: col},
			Construct: fmt.Sprintf("non-ASCII byte in %s", filepath.Base(path)),
			Context:   "SQL files",
			Hint:      hint,
		}
	}
	return nil
}
