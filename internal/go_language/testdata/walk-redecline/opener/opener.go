// Package opener compares Open's error with nil, and forwards it.
package opener

import "example.com/walkredecline/platform"

// Failed is whether Open failed.
func Failed() bool { return platform.Open() != nil }

// Get is Open's error.
func Get() error { return platform.Open() }
