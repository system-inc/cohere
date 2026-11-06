package am04

import (
	"slices"
)

func largest(values []int) int {
	return slices.MaxFunc(values, func(a, b int) int { return a - b })
}
