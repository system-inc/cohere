// Package fmt_test stands where the fmt package's own external tests do, by its import path, which the original
// spares a discarded fmt.Sprintf in, for the fmt package's benchmarks; any other pure call is still reported.
package fmt_test

import (
	"fmt"
	"strings"
)

func spared() {
	fmt.Sprintf("%d", 1)
	strings.ToUpper("still reported")
}
