package httpx

import (
	"reflect"
	"time"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
)

// ClockRule is how Bind fills an Input field tagged `clock:"now"` (quoted by
// bridge-en, grammar T1). Feature code never reads the clock: the current
// time is an input like any other, so checks can pass any time they like by
// calling Handle directly, and the English can say what a time rule means.
const ClockRule = "set by the server to the current time when the request arrives, in whole seconds since 1970-01-01 UTC; the caller does not send it, and a request that does is answered with HTTP 400 below"

// Now is the server clock behind every `clock:"now"` field. It is the only
// clock read on the request path; tests replace it to inject a time.
var Now = time.Now

// clockTag is the struct tag that marks a server-set time field.
const clockTag = "clock"

// isClockField reports whether f is filled by the server clock.
func isClockField(f reflect.StructField) bool { return f.Tag.Get(clockTag) != "" }

// stampClock sets every `clock:"now"` int64 field of *in to Now() in unix
// seconds. Any other clock field is a bug in the slice (bridge-en refuses it
// too): an assertion, so HTTP 500.
func stampClock[I any](in *I) {
	v := reflect.ValueOf(in).Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !isClockField(f) {
			continue
		}
		assert.Pre(f.Tag.Get(clockTag) == "now" && f.Type.Kind() == reflect.Int64, "a clock field is int64 tagged clock:\"now\"")
		v.Field(i).SetInt(Now().Unix())
	}
}
