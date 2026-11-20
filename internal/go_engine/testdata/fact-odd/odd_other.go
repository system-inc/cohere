//go:build !windows

package odd

// platformPairs takes any number of values, after saying how many.
func platformPairs(values []anything) {
	println("pairs", len(values))
}

// platformCount says how many values it has and needs nothing of them.
func platformCount(values []anything) {
	println("count", len(values))
}
