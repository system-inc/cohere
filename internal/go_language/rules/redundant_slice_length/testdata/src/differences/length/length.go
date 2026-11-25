// Package length declares its own len, so no len here is the builtin, and neither side reports.
package length

func len(values []int) int { return 0 }

func packageLength(values []int) {
	_ = values[:len(values)]
}
