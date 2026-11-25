// Package throughmethod reaches sort.IntSlice through aliases' alias as through does, and calls a method sort
// declares, so the type index holds sort and the original walks it.
package throughmethod

import "parity/aliases"

func converted(values []int) {
	values = aliases.Ints(values)
	aliases.Ints(values).Sort()
}
