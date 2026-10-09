package page

import (
	"math"
	"testing"
)

// TestPageSizeLimits proves how bridge-en reads IsPageLimit: n is between 1
// and MaxPageSize, both included. It pins the relations, not the numbers.
func TestPageSizeLimits(t *testing.T) {
	if DefaultPageSize < 1 || DefaultPageSize > MaxPageSize || StartCursor != math.MaxInt64 {
		t.Fatalf("page constants inconsistent: max=%d default=%d start=%d", MaxPageSize, DefaultPageSize, StartCursor)
	}
	for _, n := range []int64{1, DefaultPageSize, MaxPageSize} {
		if !IsPageLimit(n) {
			t.Fatalf("%d should be allowed", n)
		}
	}
	for _, n := range []int64{0, -1, MaxPageSize + 1} {
		if IsPageLimit(n) {
			t.Fatalf("%d should be refused", n)
		}
	}
}
