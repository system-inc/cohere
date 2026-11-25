// Package imported names an imported package copy, in the scope of the file that imports it.
package imported

import copy "strings"

var _ = copy.ToUpper

func slide(values []int, n int, offset int) {
	// The fix would call a package, which fails to compile.
	for i := 0; i < n; i++ { // port withholds: Use copy() instead of loop
		values[i] = values[offset+i]
	}
}
