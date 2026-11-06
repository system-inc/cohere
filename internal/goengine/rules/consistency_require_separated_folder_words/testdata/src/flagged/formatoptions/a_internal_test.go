// An in-package test comes first by name, and the finding passes over it to the first file of the package itself.
package formatoptions

import "testing"

func TestWidth(test *testing.T) {
	test.Parallel()
	if Width != 80 {
		test.Fatal("Width changed")
	}
}
