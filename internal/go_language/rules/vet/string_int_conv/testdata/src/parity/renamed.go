package parity

// This file imports fmt under another name, which the first fix names.

import format "fmt"

func renamed(number int) {
	_ = format.Sprint(number)
	_ = string(number)
}
