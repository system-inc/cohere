//go:build go1.19

package parity_older

import (
	"io/ioutil" // Found by both: the file is tagged for Go 1.19, whose library deprecated it.
	"strings"
)

func taggedNewer() {
	// Found by both: deprecated since Go 1.18.
	_ = strings.Title("x")
	// Found by neither: a member of a deprecated package that is not deprecated itself.
	_, _ = ioutil.ReadAll(nil)
}
