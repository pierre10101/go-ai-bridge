// Package bridge holds the bridge-en release version. The compiler is in
// adapter/, the CLI in cmd/bridge-en and the runtime apps import in runtime/.
package bridge

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version is this module's bridge-en release (the VERSION file). A release is
// the tag v<Version>; `bridge-en -version` prints it, and an app pins it by
// requiring github.com/pierre10101/go-ai-bridge v<Version> in its go.mod.
func Version() string { return strings.TrimSpace(version) }
