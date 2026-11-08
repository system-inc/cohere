// Package parity holds every import shape `ST1001` reads, each found and each near miss, for the original and the port
// to judge alike.
package parity

import (
	// Each dot import is reported, over the whole import.
	. "math"
	. "sort"
	. "strings"

	// A blank import and a named one are no dot imports.
	_ "fmt"
	format "fmt"
)

var (
	_ = ToUpper
	_ = Abs
	_ = Ints
	_ = format.Sprint
)
