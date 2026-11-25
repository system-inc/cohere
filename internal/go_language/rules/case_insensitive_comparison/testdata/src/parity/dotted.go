package parity

import . "strings"

func dotted(first string, second string) {
	_ = ToLower(first) == ToLower(second)
	_ = ToUpper(first) != ToUpper(second)
	_ = ToLower(first) == ToUpper(second)
}
