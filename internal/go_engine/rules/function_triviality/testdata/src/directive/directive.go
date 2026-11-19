// Package directive declares a function under a //line directive, for the test of the file an answer names it by.
package directive

// Asked is trivial through moved.
func Asked() error { return moved() }

// moved is declared in this file, and the directive below says its line is line 10 of other.go.
//
//line other.go:10
func moved() error { return nil }
