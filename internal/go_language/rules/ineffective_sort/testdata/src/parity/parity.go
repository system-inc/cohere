// Package parity holds the assignments SA4029 judges, flagged and left alone, for the port and the original to judge
// side by side: each of sort's three slice types, parentheses around every part, a renamed import and package-level
// aliases, and the assignments the pattern or the original refuses for their target, their argument, their operator
// or a conversion that is no sort type. A target of a named slice type or a type parameter cannot hold the
// conversion's result, so no such assignment type-checks.
package parity

import (
	"sort"
	sorting "sort"
)

type ints = sort.IntSlice

type aliasOfAlias = ints

type holder struct {
	values []int
}

type Strings = []string

func flagged(numbers []int, words []string, decimals []float64, aliased Strings) {
	numbers = sort.IntSlice(numbers)
	words = sort.StringSlice(words)
	decimals = sort.Float64Slice(decimals)
	aliased = sort.StringSlice(aliased)
	(numbers) = sort.IntSlice(numbers)
	numbers = (sort.IntSlice)(numbers)
	numbers = sort.IntSlice((numbers))
	numbers = (sort.IntSlice(numbers))
	numbers = sorting.IntSlice(numbers)
	numbers = ints(numbers)
	numbers = aliasOfAlias(numbers)
	func() {
		words = sort.StringSlice(words)
	}()
	if len(numbers) > 0 {
		decimals = sort.Float64Slice(decimals)
	}
}

func notFlagged(numbers []int, other []int, own sort.IntSlice, box holder, decimals []float64) {
	type local = sort.IntSlice
	numbers = local(numbers)
	own = sort.IntSlice(own)
	box.values = sort.IntSlice(box.values)
	numbers = sort.IntSlice(other)
	numbers = sort.IntSlice(numbers[:])
	numbers, other = sort.IntSlice(numbers), other
	numbers = []int(numbers)
	decimals = sort.Float64Slice(decimals[1:])
	sort.Sort(sort.IntSlice(numbers))
	{
		numbers := sort.IntSlice(numbers)
		_ = numbers
	}
	numbers = append(sort.IntSlice(numbers), 1)
	_, _, _ = own, box, decimals
}
