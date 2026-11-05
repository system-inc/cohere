//go:build ignore

// This file is built for no target, so its printf finding is never reported.
package constrained

import "fmt"

func report() {
	fmt.Printf("%d\n", "three")
}
