// Fixture app for the adapter tests. It is laid out like an app that depends
// on bridge-en: slices import <module>/features/<slice>/db, the domain is
// internal/domain, and cmd/server binds the routes with the runtime. The
// slices live under good/ and bad/ instead of features/; nothing here is
// built (testdata is ignored by the go command), bridge-en only reads it.
module example.com/fixtures

go 1.24.0

require github.com/pierre10101/go-ai-bridge v0.6.0

replace github.com/pierre10101/go-ai-bridge => ../..
