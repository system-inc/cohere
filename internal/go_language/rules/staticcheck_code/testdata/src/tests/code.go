// Package tests holds nodes in a test file and in other files, for the test of IsInTest: this file, an in-package
// test file, an external test file, and a file and a test file whose line directives name the other kind.
package tests

type record struct {
	value int
}

func (receiver record) total(other int) int { return receiver.value + other }

var initialized = func() int { return 1 }()
