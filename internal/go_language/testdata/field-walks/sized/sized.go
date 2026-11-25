// Package sized hands each type to a non-empty interface, through which no rule looks.
package sized

import "example.com/fieldwalks/platform"

// sizer is anything with a Size.
type sizer interface{ Size() int }

// total is every size.
func total(values ...sizer) int {
	sum := 0
	for _, value := range values {
		sum += value.Size()
	}
	return sum
}

// Total hands every type to sizer.
func Total(
	printed *platform.Printed,
	locked *platform.Locked,
	encoded *platform.Encoded,
	hidden *platform.Hidden,
	channeled *platform.Channeled,
) int {
	return total(printed, locked, encoded, hidden, channeled)
}
