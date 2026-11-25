// Package below declares the check package subject's marks rest on, across a package.
package below

// Pairs panics unless its values come in pairs.
func Pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}
