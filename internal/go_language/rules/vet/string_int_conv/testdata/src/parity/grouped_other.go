package parity

// This file's group opens with a package whose path is not a standard one, so the first fix adds fmt's spec before
// it, followed by a blank line. Its conversions name types from that package, by selector.

import (
	"example.com/library"
	"strings"
)

func groupedOther(number int, count library.Count, total library.Number) {
	_ = strings.ToUpper(string(number))
	_ = library.Text(number)
	_ = library.Label(number)
	_ = string(count)
	_ = string(total)
	_ = library.Text(count)
	_ = string(library.Value)
	_ = string(library.Count(3))
	_ = library.Converted(count)
}
