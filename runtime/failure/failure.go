// Package failure defines the business failure type. Every failure has a
// stable ID (F1, F2, ...) that matches the slice's intent.md and its checks.
package failure

// Failure is an expected, user-facing failure of an action.
type Failure struct {
	ID      string
	Status  int
	Message string
}

// New declares a failure case. Call it only in a slice's F-ID var block.
func New(id string, status int, message string) *Failure {
	return &Failure{ID: id, Status: status, Message: message}
}

func (f *Failure) Error() string { return f.ID + ": " + f.Message }
