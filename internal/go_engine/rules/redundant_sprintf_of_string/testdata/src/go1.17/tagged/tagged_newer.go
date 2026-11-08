//go:build go1.18

package tagged

import "fmt"

// The build line sets this file's language to go1.18, past the module's, so the slice converts here.
func newer(bytes []Byte) {
	_ = fmt.Sprintf("%s", bytes)
}
