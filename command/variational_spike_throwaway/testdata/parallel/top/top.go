// Package top imports both sides of the diamond.
package top

import (
	"errors"
	"fmt"
	"io"

	"example.com/parallel/left"
	"example.com/parallel/right"
	"example.com/parallel/shape"
)

// errNoShapes is Report's error for no shapes.
var errNoShapes = errors.New("no shapes")

// Report writes every shape, the longest and the tally.
func Report(writer io.Writer, shapes []shape.Shape) error {
	if len(shapes) == 0 {
		return errNoShapes
	}
	tally := &right.Tally{}
	for _, each := range shapes {
		tally.Add(each)
	}
	if writeError := left.Write(writer, shapes); writeError != nil {
		return fmt.Errorf("writing: %w", writeError)
	}
	_, printError := fmt.Fprintf(writer, "longest %v, tally %s\n", left.Longest(shapes), tally)
	return printError
}
