//go:build !windows

package odd

// platformPairs takes any number of values, after saying how many.
func platformPairs(values []any) {
	println("pairs", len(values))
}
