// Package shadowed declares a copy at package scope, which every loop in it stands in the scope of.
package shadowed

func copy(to []int, from []int) int { return len(to) + len(from) }

func slide(values []int, n int, offset int) {
	// The fix would call the package's copy, which copies nothing.
	for i := 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
}
