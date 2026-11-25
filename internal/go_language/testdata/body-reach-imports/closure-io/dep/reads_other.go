//go:build !windows

package dep

import _ "io"

// Gating is the host's here, windows's twin imports nothing.
const Gating = true
