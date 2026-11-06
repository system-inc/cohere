package am03

import (
	"slices"
)

func sortNumbers(values []int) {
	slices.SortFunc(values, func(a, b int) int { return a - b })
}
