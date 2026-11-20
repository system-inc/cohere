//go:build !windows

package odd

// platformPairs takes any number of values, after saying how many.
func platformPairs(values []any) {
	println("pairs", len(values))
}

// platformCount says how many values it has and needs nothing of them.
func platformCount(values []any) {
	println("count", len(values))
}
