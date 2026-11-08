// Package dotted imports fmt and log with dots, one file each, so their functions are named by identifiers alone.
package dotted

import (
	"io"

	. "fmt"
)

// A dot import names fmt's functions by identifiers alone, which the original's Symbol query resolves as it does a
// selector, and its messages still name fmt.
func dotFormat(writer io.Writer) {
	Print(Sprintf("%d", 1))            // reported
	Println(Sprintf("%d", 1))          // reported
	Fprintln(writer, Sprintf("%d", 1)) // reported
	Println(Sprintf(layout, 1))        // left alone
	(Print)((Sprintf)("%d", 1))        // reported
}

const layout = "%d"
