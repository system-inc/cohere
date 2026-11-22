// Package records is the build's package the standing_facts tests of U1000's records settle: each body's record is
// usable, or unusable for one reason, as its comment says.
package records

import "unsafe"

type inner struct{ value int }

type target struct{ held inner }

type pointer *target

// Address converts a named pointer to unsafe.Pointer, which uses every field of what it points to, so U1000's record
// of it is flagged and never spliced.
func Address(value pointer) unsafe.Pointer { return unsafe.Pointer(value) }

// Answer builds a type types.go declares, so its record names types.go past its own file.
func Answer() string {
	cases := []answerCase{{path: "a"}}
	return cases[0].path
}

// Missing is a body the host's side answers no record of.
func Missing() int { return 2 }

// Stale is a body whose own file the host's side digests otherwise.
func Stale() int { return 3 }

// Plain names nothing past its own file, so its record is usable.
func Plain() int { return 1 }
