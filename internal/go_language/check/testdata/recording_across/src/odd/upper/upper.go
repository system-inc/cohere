// Package upper is asked of its functions' odd slice marks, which rest on lower.Pairs's, a function of another package.
package upper

import "odd/lower"

// wrap hands its values to lower.Pairs, so they must come in pairs only where Pairs needs them to.
func wrap(values []string) {
	lower.Pairs(values)
}

// top hands its values to wrap.
func top(values []string) {
	wrap(values)
}
