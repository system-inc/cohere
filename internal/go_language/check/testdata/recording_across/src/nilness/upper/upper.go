// Package upper is asked of its functions' nilness, which rests on lower.Middle's, a function of another package.
package upper

import "nilness/lower"

// wrap returns lower.Middle's error, never nil only where Middle's is not.
func wrap() error {
	return lower.Middle()
}

// top returns wrap's error.
func top() error {
	return wrap()
}
