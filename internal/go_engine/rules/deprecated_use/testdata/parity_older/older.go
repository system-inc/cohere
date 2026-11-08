// Package parity_older uses the standard library's deprecations from a module of Go 1.15, where those that came later
// are left alone, and a file tagged for a newer version uses its tag's library.
package parity_older

import (
	"io/ioutil" // Found by neither: deprecated since Go 1.19, after the module's version.
	"os"
	"strings"
)

func older() {
	// Found by both: deprecated since Go 1.7.
	_ = os.SEEK_SET
	// Found by neither: deprecated since Go 1.18, after the module's version, and a member of a deprecated package.
	_ = strings.Title("x")
	_, _ = ioutil.ReadAll(nil)
}
