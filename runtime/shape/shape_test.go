package shape

import "testing"

// TestHas proves how bridge-en reads the primitive: '#' is exactly one
// digit 0-9, every other character is itself, and the length must match.
func TestHas(t *testing.T) {
	for _, c := range []struct {
		s, shape string
		want     bool
	}{
		{"INV-000042", "INV-######", true},
		{"INV-00004", "INV-######", false},
		{"INV-0000421", "INV-######", false},
		{"INV-00004x", "INV-######", false},
		{"INX-000042", "INV-######", false},
		{"#", "#", false},
		{"7", "#", true},
		{"", "", true},
	} {
		if got := Has(c.s, c.shape); got != c.want {
			t.Errorf("Has(%q, %q) = %v, want %v", c.s, c.shape, got, c.want)
		}
	}
}
