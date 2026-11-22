// Package goengine sits in a folder that runs its words together, and its one body, which the strip takes away, is
// the only user of strings.
package goengine

import "strings"

func trimmed() string {
	return strings.TrimSpace(" engine ")
}
