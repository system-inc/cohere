// Package method calls a method sort declares through aliased's alias, and a method is no package-level object, so
// the index holds sort beside aliased.
package method

import "indexed/aliased"

const Value = 1

func sorted(values aliased.Ints) {
	values.Sort()
}
