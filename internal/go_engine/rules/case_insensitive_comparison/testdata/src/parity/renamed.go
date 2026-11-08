package parity

import lettering "strings"

func renamed(first string, second string) {
	_ = lettering.ToLower(first) == lettering.ToLower(second)
	_ = lettering.ToUpper(first) != lettering.ToUpper(second)
}
