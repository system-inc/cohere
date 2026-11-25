// Package reader reads only what every build of platform declares alike.
package reader

import "example.com/restore/platform"

// Version is platform's.
func Version() string { return platform.Version }
