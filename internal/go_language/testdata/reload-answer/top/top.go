// Package top reads bottom's and side's constants and names os.Getpagesize, and once the test's edit adds Drop, calls
// bottom.Double, side.Triple and os.Getpagesize and drops their results.
package top

import (
	"os"

	"example.com/reloadanswer/bottom"
	"example.com/reloadanswer/side"
)

// Pages is os's page size, named so top imports os before the edit calls it.
var Pages = os.Getpagesize

// Limit is bottom's and side's.
func Limit() int {
	return bottom.Limit * side.Factor
}
