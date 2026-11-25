// Package aliased imports sort and declares an alias of one of its types, for the packages that reach sort only
// through it.
package aliased

import "sort"

const Value = 1

type Ints = sort.IntSlice
