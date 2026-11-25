// Package screaming is the build's package the screaming presence test settles: it imports syscall, so each constant
// is in a system file, and each says beside it whether the row's predicate holds of it.
package screaming

import "syscall"

// O_RDONLY is screaming's candidate: underscored, in a file importing syscall.
const O_RDONLY = syscall.O_RDONLY

// ReadOnly is no candidate: it has no underscore.
const ReadOnly = syscall.O_RDONLY

// Reads names O_RDONLY.
func Reads(flags int) bool { return flags == O_RDONLY }

// Writes names no constant.
func Writes() int { return 1 }

// Plain names ReadOnly.
func Plain(flags int) bool { return flags == ReadOnly }
