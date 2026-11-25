// Package reader compares platform's settings' part with itself, which reads the part's layout.
package reader

import "example.com/aliasrecords/platform"

// Same reports whether the settings' part equals a copy of it.
func Same() bool {
	part := platform.Settings.Part
	return part == platform.Settings.Part
}
