package return_nilness

import "github.com/system-inc/cohere/internal/go_engine/intermediate_representation"

// numbering numbers the values of one function densely, in the order they are first asked about, shared by every
// state of the function so a number means one value throughout.
type numbering map[intermediate_representation.Value]int

// number is value's number, given the next one when value has none yet.
func (values numbering) number(value intermediate_representation.Value) int {
	number, numbered := values[value]
	if !numbered {
		number = len(values)
		values[value] = number
	}
	return number
}
