package assert

import "testing"

func TestPrePanicsWithViolation(t *testing.T) {
	defer func() {
		v, ok := recover().(Violation)
		if !ok || v.Kind != "precondition" || v.Message != "x is positive" {
			t.Fatalf("want precondition violation, got %#v", v)
		}
	}()
	Pre(false, "x is positive")
}

func TestPostPassesWhenTrue(t *testing.T) {
	Post(true, "never fires")
}
