// Package dotted imports sort into its own scope, where the pattern's Symbol still names sort's types.
package dotted

import . "sort"

func dotted(values []int) {
	values = IntSlice(values)
	_ = values
}
