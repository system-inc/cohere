// Package middle imports bottom, whose exported function's fact it holds.
package middle

import "demand/hops/bottom"

// Middle is pure, through bottom.Pure, which middle holds pure.
func Middle(value int) int {
	return bottom.Pure(value) + 1
}
