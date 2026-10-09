// Package shape is the one text primitive a domain function may call
// (grammar M3): Has(s, "INV-######").
package shape

// Has reports whether s is exactly shape, where each '#' in shape is one
// ASCII digit and every other byte stands for itself.
//
// It is a bridge-en primitive (grammar M3): bridge-en does not read this body,
// it renders shape.Has(s, "INV-######") as "s is INV- followed by six digits".
// TestHas proves that reading.
func Has(s, shape string) bool {
	if len(s) != len(shape) {
		return false
	}
	for i := 0; i < len(shape); i++ {
		switch {
		case shape[i] == '#':
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		case s[i] != shape[i]:
			return false
		}
	}
	return true
}
