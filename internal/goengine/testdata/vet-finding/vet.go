// Package vet holds one call vet's printf check finds.
package vet

import "fmt"

// Report formats a count with a verb that does not fit it.
func Report() {
	_ = fmt.Sprintf("%d\n", "three")
}
