package parity

// This file imports fmt for its effects alone, under the blank name, which no reference can use, so the first fix
// imports it again.

import _ "fmt"

func blank(number int) {
	_ = string(number)
}
