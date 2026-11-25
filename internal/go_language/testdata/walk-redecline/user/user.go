// Package user compares Get's error with nil, and forwards it.
package user

import "example.com/walkredecline/opener"

// Failed is whether Get failed.
func Failed() bool { return opener.Get() != nil }

// Again is Get's error.
func Again() error { return opener.Get() }
