package httpx

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/pierre10101/go-ai-bridge/runtime/page"
)

// BadQueryWhen is when Bind answers BadInput for a GET (quoted by bridge-en).
const BadQueryWhen = "a path value is missing, a value is not a whole number, a query value appears more than once, or the query string has a parameter not listed above (names are case-sensitive)"

// StrictQueryRule is how Bind treats the query string of a GET (quoted by
// bridge-en, grammar T4): the same strictness as a JSON body, which may
// not have a field that is not listed. unknownQuery enforces it.
const StrictQueryRule = "The query string is as strict as a body: a query parameter that is not listed above (names are case-sensitive) is answered with HTTP 400 below and the action does not run."

// QueryInputRule is how Bind treats GET path and query inputs (quoted by
// bridge-en). The page sizes themselves live only in runtime/page
// (MaxPageSize, DefaultPageSize); bridge-en states them from there.
const QueryInputRule = "Path values are required. A query value may be left out: `limit` is then the default page size and `after` starts the list at the newest row. A missing path value, or a value that is not a whole number, is answered with HTTP 400 below and the action does not run."

// decodeParams fills I from path and query struct tags for GET requests.
// Tags: `path:"id"` and `query:"limit"`. json tags still name the fields in English.
func hasParamTags[I any]() bool {
	var zero I
	t := reflect.TypeOf(zero)
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("path") != "" || f.Tag.Get("query") != "" || isServerSet(f) {
			return true
		}
	}
	return false
}

func decodeParams[I any](r *http.Request) (I, string) {
	var in I
	v := reflect.ValueOf(&in).Elem()
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return in, "internal: input type must be a struct"
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fv := v.Field(i)
		if isServerSet(f) {
			continue // ClockRule, SessionRule: Bind fills it after decoding
		}
		if pathName := f.Tag.Get("path"); pathName != "" {
			raw := r.PathValue(pathName)
			if raw == "" {
				return in, fmt.Sprintf("required path parameter %q is missing", pathName)
			}
			if msg := setInt64Field(fv, raw, "path "+pathName); msg != "" {
				return in, msg
			}
			continue
		}
		if queryName := f.Tag.Get("query"); queryName != "" {
			raw, ok := r.URL.Query()[queryName]
			if !ok || len(raw) == 0 || raw[0] == "" {
				// Defaults for list paging.
				switch queryName {
				case "limit":
					fv.SetInt(page.DefaultPageSize)
				case "after":
					fv.SetInt(page.StartCursor)
				default:
					return in, fmt.Sprintf("required query parameter %q is missing", queryName)
				}
				continue
			}
			if len(raw) != 1 {
				return in, fmt.Sprintf("query parameter %q must appear once", queryName)
			}
			if msg := setInt64Field(fv, raw[0], "query "+queryName); msg != "" {
				return in, msg
			}
			continue
		}
		return in, fmt.Sprintf("GET input field %q needs a path or query struct tag", f.Name)
	}
	return in, ""
}

// unknownQuery returns a message when a GET request's query string has a
// parameter that no Input field of I declares with a query tag, compared
// case-sensitively like body field names (StrictQueryRule); "" otherwise.
// A server-set name is answered by serverSetSent first, with its own message.
func unknownQuery[I any](r *http.Request) string {
	declared := map[string]bool{}
	if t := reflect.TypeOf(*new(I)); t != nil && t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			if q := t.Field(i).Tag.Get("query"); q != "" && !isServerSet(t.Field(i)) {
				declared[q] = true
			}
		}
	}
	var unknown []string
	for k := range r.URL.Query() {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	return fmt.Sprintf("query parameter %q is not one this action takes", unknown[0])
}

func setInt64Field(fv reflect.Value, raw, what string) string {
	if fv.Kind() != reflect.Int64 {
		return fmt.Sprintf("%s: only int64 path/query fields are supported", what)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return fmt.Sprintf("%s must be a whole number", what)
	}
	fv.SetInt(n)
	return ""
}
