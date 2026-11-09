// Package top reads bottom's constant and names os.Getpagesize, and once the test's edit adds Drop, calls both
// bottom.Double and os.Getpagesize and drops their results.
package top

import (
	"os"

	"example.com/reloadanswer/bottom"
)

// Pages is os's page size, named so top imports os before the edit calls it.
var Pages = os.Getpagesize

// Limit is bottom's.
func Limit() int {
	return bottom.Limit
}
