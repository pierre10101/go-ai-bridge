// Package domain holds the sanctioned shared value objects and pure rules.
// No I/O, no framework calls. Slices import it; nothing is duplicated.
//
// Every function here is under the bridge-en grammar (M2, M3): bridge-en
// renders its English from the body, so the English changes when the code
// does. A `// bridge-en:` line gives only a type's display name.
package domain

// Money is an amount in minor units (cents) and an ISO 4217 currency code.
//
// bridge-en: an amount of money
type Money struct {
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"`
}

// IsSupportedCurrency reports whether we invoice in this currency.
func IsSupportedCurrency(code string) bool {
	switch code {
	case "ZAR", "USD", "EUR":
		return true
	}
	return false
}
