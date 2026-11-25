//go:build !windows

package gate

// The host's in-package test file imports unsafe, so unusedwrite stands down in the host's test variant of gate, where
// the host lints shared.go, and windows, with no test file, lints shared.go in gate itself and reports Target.
import _ "unsafe"
