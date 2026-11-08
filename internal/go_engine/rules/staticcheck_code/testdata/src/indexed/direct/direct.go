// Package direct imports sort and calls a function of it, so the index holds sort.
package direct

import "sort"

const Value = 1

func sorted(values []int) {
	sort.Ints(values)
}
