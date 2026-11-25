package parity

import . "parity/constants"

// dotted names another package's constants and variable through a dot import, as identifiers.
func dotted() {
	var x bool
	_ = x == On   // reported: x
	_ = Off != x  // reported: x
	_ = x == Flag // not reported: a variable is no constant
}
