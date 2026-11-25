// Package through reaches sort's type only through aliased's alias, a package-level name of aliased, so the index
// holds aliased and not sort.
package through

import "indexed/aliased"

const Value = 1

func converted(values []int) aliased.Ints {
	return aliased.Ints(values)
}
