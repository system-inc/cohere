// Package left reads shapes through io and sorts them with slices.
package left

import (
	"bytes"
	"io"
	"slices"
	"strings"

	"example.com/parallel/shape"
)

// Longest is the shape with the most sides.
func Longest(shapes []shape.Shape) shape.Shape {
	return slices.MaxFunc(shapes, func(a shape.Shape, b shape.Shape) int { return len(a.Sides) - len(b.Sides) })
}

// Write writes each shape on its line.
func Write(writer io.Writer, shapes []shape.Shape) error {
	var buffer bytes.Buffer
	for _, each := range shapes {
		buffer.WriteString(strings.TrimSpace(each.String()))
		buffer.WriteByte('\n')
	}
	_, writeError := buffer.WriteTo(writer)
	return writeError
}
