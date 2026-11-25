//go:build !windows

package dep

import "hash"

// Hash is hash's, a typed use so the import holds where dep comes from export data, whose package lists only the
// imports its exported declarations reach.
type Hash = hash.Hash

// Gating is the host's here, windows's twin imports nothing.
const Gating = true
