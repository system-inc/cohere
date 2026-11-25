// Package through reaches sort.IntSlice only through aliases' alias and uses nothing sort declares, so staticcheck's
// type index of it holds no sort and the original walks nothing here, though the pattern would match.
package through

import "parity/aliases"

func converted(values []int) {
	values = aliases.Ints(values)
	_ = values
}
