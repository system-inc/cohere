// A second file of the external test package carries no finding of its own.
package formatoptions_test

import (
	"testing"

	"flagged/formatoptions"
)

func TestWidthAgain(test *testing.T) {
	test.Parallel()
	if formatoptions.Width != 80 {
		test.Fatal("Width changed")
	}
}
