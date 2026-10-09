// Package page computes the keyset cursor of the next page of a list.
//
// It is plumbing outside features/ (reflection is fine here), and bridge-en
// does not read its body: NextAfter is a primitive with one fixed English
// reading, which the tests in this package prove:
//
//	the `<key>` of the last listed <row> if the page is full (it has <limit>
//	of them), otherwise 0: there is no next page.
package page

import (
	"reflect"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// NextAfter returns the cursor for the page after rows: the value of column
// key in the last row when len(rows) == limit, else 0. rows is a slice of
// sqlc row structs; key is the SQL column name (seq), matched to the Go field
// the way sqlc names it (case and underscores ignored).
func NextAfter[R any](rows []R, key string, limit int64) int64 {
	assert.Pre(limit >= 1, "page limit is positive")
	assert.Pre(int64(len(rows)) <= limit, "a page never has more rows than its limit")
	if int64(len(rows)) < limit {
		return 0
	}
	last := reflect.ValueOf(rows[len(rows)-1])
	assert.Pre(last.Kind() == reflect.Struct, "rows are structs")
	want := norm(key)
	for i := 0; i < last.NumField(); i++ {
		if norm(last.Type().Field(i).Name) == want && last.Field(i).Kind() == reflect.Int64 {
			v := last.Field(i).Int()
			assert.Post(v > 0, "a keyset cursor is positive")
			return v
		}
	}
	assert.Pre(false, "the row has the int64 key column "+key)
	return 0
}

func norm(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
