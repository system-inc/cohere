// Package reader compares two values platform makes, which reads their layout.
package reader

import "example.com/records/platform"

// Same reports whether two made values are equal.
func Same() bool {
	first, second := platform.Make(), platform.Make()
	return first == second
}
