// Package differences holds the assignments where the port's message and fix and the original's could most plausibly
// print differently, and records no difference, since they part nowhere: conversions spread over lines, beside
// comments and inside parentheses.
package differences

import "sort"

func plausible(numbers []int, words []string) {
	numbers = sort.IntSlice(
		numbers,
	)
	words = sort.StringSlice(words /* the words */)
	numbers = ((sort.IntSlice)((numbers)))
	words = // sorted below
		sort.StringSlice(words)
	_, _ = numbers, words
}
