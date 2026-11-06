// Package dotimport imports strings with a dot, which ST1001 reports unless a staticcheck.conf above it allows it.
package dotimport

import . "strings"

// Shout is the dot import's one use.
func Shout(text string) string {
	return ToUpper(text)
}
