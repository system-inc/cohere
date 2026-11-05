// Package vet holds one call vet's printf check finds.
package vet

import "fmt"

// Report prints a count with a verb that does not fit it.
func Report() {
	fmt.Printf("%d\n", "three")
}
