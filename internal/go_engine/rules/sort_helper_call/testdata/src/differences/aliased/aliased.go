// Package aliased gives package sort's helper an alias of its own, which the differences fixture converts to.
package aliased

import "sort"

// Text is sort.StringSlice.
type Text = sort.StringSlice
