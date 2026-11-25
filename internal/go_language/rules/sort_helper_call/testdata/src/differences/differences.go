// Package differences holds every finding the port makes and the original does not: a conversion to a helper
// spelled other than as package sort's selector, which the original takes for a type of the code's own, so it
// reports neither that call nor any other in its function.
package differences

import (
	"sort"

	"differences/aliased"
)

// ints is sort.IntSlice by another name.
type ints = sort.IntSlice

// The original sees through no parentheses around the argument.
func parenthesizedArgument(v []int) {
	sort.Sort((sort.IntSlice(v))) // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))
}

// Nor around the type it converts to.
func parenthesizedType(v []int) {
	sort.Sort((sort.IntSlice)(v)) // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))
}

// A local alias is an identifier, no selector.
func localAlias(v []int) {
	sort.Sort(ints(v)) // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))
}

// Another package's alias is a selector the original names by that package.
func packageAlias(v []string) {
	sort.Sort(aliased.Text(v)) // port finds: should use sort.Strings(...) instead of sort.Sort(sort.StringSlice(...))
}

// The original holds the function permissible for the alias, and reports the plainly spelled call beside it neither.
func silencedBeside(v []int) {
	sort.Sort(sort.IntSlice(v)) // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))
	sort.Sort(ints(v))          // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))
}
