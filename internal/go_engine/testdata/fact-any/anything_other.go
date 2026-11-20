//go:build !windows

package anything

// platformPairs takes any number of values, after saying how many.
func platformPairs(values []any) {
	println("pairs", len(values))
}
