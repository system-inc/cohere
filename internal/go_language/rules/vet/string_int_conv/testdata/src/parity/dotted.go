package parity

// This file dot-imports fmt, so the first fix calls Sprint unqualified, except where a local Sprint shadows it, and
// fmt is imported by name.

import . "fmt"

func dotted(number int) {
	_ = Sprint(number)
	_ = string(number)
	_ = Text(number)
}

func dottedShadowed(number int) {
	Sprint := number
	_ = string(Sprint)
}
