// Package shape is the root of the parallel fixture's diamond: left and right import it, and top imports both.
package shape

import (
	"fmt"
	"strings"
)

// Shape is a named shape with sides.
type Shape struct {
	Name  string
	Sides []int
}

// String spells the shape.
func (shape Shape) String() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s:", shape.Name)
	for _, side := range shape.Sides {
		fmt.Fprintf(&builder, " %d", side)
	}
	return builder.String()
}

var _ fmt.Stringer = Shape{}
