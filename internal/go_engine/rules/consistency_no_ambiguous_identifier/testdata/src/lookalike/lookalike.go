package lookalike

import (
	// An import's name is outside the rule's list of declarations, so a letter here is left alone.
	e "errors"
	"testing"
)

func wrap() error {
	return e.New("wrapped")
}

// An embedded field is named by its type, testing's T, which is judged where testing declares it.
type suite struct {
	*testing.T
}

// A label names a place to jump, outside the rule's list of declarations.
func firstNegative(rows [][]int) int {
	found := 0
L:
	for _, row := range rows {
		for _, cell := range row {
			if cell < 0 {
				found = cell
				break L
			}
		}
	}
	return found
}

// A type switch with no name binds nothing.
func kind(value any) string {
	switch value.(type) {
	case int:
		return "int"
	}
	return ""
}

// Two letters are a short name, not a single letter.
func positive(id int, ok bool) bool {
	return id > 0 && ok
}
