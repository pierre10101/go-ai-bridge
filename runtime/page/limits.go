package page

// Page-size hard limits. bridge-en states both in every list contract; the
// adapter refuses a :many query without LIMIT and treats a limit outside
// 1..MaxPageSize as a failure case (not a silent clamp).
const (
	MaxPageSize     int64 = 100
	DefaultPageSize int64 = 20
	// StartCursor is the keyset watermark for the first (newest) page:
	// WHERE seq < StartCursor. httpx fills a missing `after` query param
	// with this value.
	StartCursor int64 = 9223372036854775807
)

// IsPageLimit reports whether n is an allowed page size (1..MaxPageSize).
//
// It is a bridge-en primitive (grammar E5): bridge-en does not read this
// body, it renders !page.IsPageLimit(x) as "x is not between 1 and
// <MaxPageSize> (both included)". TestPageSizeLimits proves that reading.
func IsPageLimit(n int64) bool {
	return n >= 1 && n <= MaxPageSize
}
