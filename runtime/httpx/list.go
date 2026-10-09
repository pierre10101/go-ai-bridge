package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// List inputs (grammar D10). An Input field of type []int64 or []string
// tagged `list:"<min>..<max>"` is a list the caller sends in the body. Bind
// refuses the request with BadInput, and the action does not run, when the
// list has fewer than <min> or more than <max> entries, has the same entry
// twice, or has an entry that is null; an entry of the wrong type is refused
// by the strict decode like any other value of the wrong type.

// ListTag is the struct tag that makes an Input slice field a list input.
const ListTag = "list"

// MaxListLen is the largest <max> a list tag may declare.
const MaxListLen = 100

// ListRule is how bridge-en describes a list input (quoted, grammar D10).
// bridge-en fills {min}, {max} and {elems} (ListElems, by element type).
// ListRuleExact is used when <min> equals <max>.
const (
	ListRule      = "a list of {min} to {max} {elems} with no duplicates"
	ListRuleExact = "a list of exactly {min} {elems} with no duplicates"
)

// ListElems names the entries of a list input, by Go element type.
var ListElems = map[string]string{
	"int64":  "whole numbers",
	"string": "text values",
}

// ListWhen is the extra BadInput condition of an action with a list input
// (quoted by bridge-en after BadInput.When).
const ListWhen = "a list has fewer or more entries than allowed above, has the same entry twice, or has an entry that is null"

// ParseListTag reads a list tag "<min>..<max>": whole numbers in digits
// only, 1 <= min <= max <= MaxListLen. bridge-en refuses a slice whose tag
// does not parse; Bind treats one as a bug in the slice (HTTP 500).
func ParseListTag(tag string) (min, max int, err error) {
	lo, hi, ok := strings.Cut(tag, "..")
	if !ok || !digits(lo) || !digits(hi) {
		return 0, 0, fmt.Errorf("list tag %q is not <min>..<max> (for example \"1..20\")", tag)
	}
	min, err1 := strconv.Atoi(lo)
	max, err2 := strconv.Atoi(hi)
	if err1 != nil || err2 != nil || min < 1 || max < min || max > MaxListLen {
		return 0, 0, fmt.Errorf("list tag %q must have 1 <= min <= max <= %d", tag, MaxListLen)
	}
	return min, max, nil
}

func digits(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isListField reports whether f is a list input: a slice field of Input.
func isListField(f reflect.StructField) bool {
	return f.Type.Kind() == reflect.Slice && !isServerSet(f)
}

// checkLists applies ListRule and ListWhen to every list field of in, whose
// JSON object is obj (already decoded strictly into in, every field present).
// A slice field that is not []int64 or []string with a valid list tag is a
// bug in the slice: an assertion, so HTTP 500.
func checkLists(in reflect.Value, obj map[string]json.RawMessage) string {
	t := in.Type()
	if t.Kind() != reflect.Struct {
		return ""
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || !isListField(f) {
			continue
		}
		ek := f.Type.Elem().Kind()
		min, max, err := ParseListTag(f.Tag.Get(ListTag))
		assert.Pre(err == nil && (ek == reflect.Int64 || ek == reflect.String),
			"a list input is []int64 or []string tagged list:\"<min>..<max>\"")
		name := fieldName(f)
		var entries []json.RawMessage
		if err := json.Unmarshal(obj[name], &entries); err != nil {
			return fmt.Sprintf("field %q must be a list", name)
		}
		for _, e := range entries {
			if string(bytes.TrimSpace(e)) == "null" {
				return fmt.Sprintf("field %q has an entry that is null", name)
			}
		}
		v := in.Field(i)
		if n := v.Len(); n < min || n > max {
			return fmt.Sprintf("field %q must have %d to %d entries, not %d", name, min, max, n)
		}
		seen := make(map[any]bool, v.Len())
		for j := 0; j < v.Len(); j++ {
			e := v.Index(j).Interface()
			if seen[e] {
				return fmt.Sprintf("field %q has the entry %s more than once", name, strconv.Quote(fmt.Sprint(e)))
			}
			seen[e] = true
		}
	}
	return ""
}
