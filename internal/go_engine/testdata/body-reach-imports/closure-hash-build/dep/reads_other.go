//go:build !windows

package dep

// Gating is the host's twin of windows's file, which imports the path the gate reads: this one imports nothing.
const Gating = false
