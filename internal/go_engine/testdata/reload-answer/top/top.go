// Package top reads bottom's constant, and once the test's edit adds Drop, calls bottom.Double and drops its result.
package top

import "example.com/reloadanswer/bottom"

// Limit is bottom's.
func Limit() int {
	return bottom.Limit
}
