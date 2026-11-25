// Package printed prints a Printed's fields with %d, through a pointer.
package printed

import (
	"fmt"

	"example.com/fieldwalks/platform"
)

// Show prints printed.
func Show(printed *platform.Printed) { fmt.Printf("%d\n", printed) }
