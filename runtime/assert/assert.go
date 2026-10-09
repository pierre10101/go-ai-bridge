// Package assert provides always-on assertions (TigerBeetle style).
//
// These are not Go's missing assert() and not test helpers: they run in
// production. A failed assertion means the code is wrong, not the user.
package assert

// Violation is the panic value raised by a failed assertion.
type Violation struct {
	Kind    string // "precondition" or "postcondition"
	Message string
}

func (v Violation) Error() string { return v.Kind + " violated: " + v.Message }

// Pre asserts a precondition. It panics with a Violation when cond is false.
func Pre(cond bool, message string) {
	if !cond {
		panic(Violation{Kind: "precondition", Message: message})
	}
}

// Post asserts a postcondition. It panics with a Violation when cond is false.
func Post(cond bool, message string) {
	if !cond {
		panic(Violation{Kind: "postcondition", Message: message})
	}
}
