package parity

import (
	. "errors"
	. "fmt"
)

// Dot imports: the callee is the package's own function under a bare name.
func dotImported() {
	Sprint(1)        // reported
	Sprintf("%d", 1) // reported
	Errorf("")       // reported
	New("")          // reported
	Println(1)       // clean
	(Sprint)(1)      // reported
}
