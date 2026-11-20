//go:build !windows

package odd

// platformPairs takes any number of values, after saying how many.
func platformPairs(values []anything) {
	println("pairs", len(values))
}
