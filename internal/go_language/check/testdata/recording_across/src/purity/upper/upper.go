// Package upper is asked of its functions' purity, which rests on lower.Middle's, a function of another package.
package upper

import "purity/lower"

// wrap is five more than lower.Middle's, pure only where Middle is.
func wrap(value int) int {
	return lower.Middle(value) + 5
}

// top is three more than wrap's.
func top(value int) int {
	return wrap(value) + 3
}
