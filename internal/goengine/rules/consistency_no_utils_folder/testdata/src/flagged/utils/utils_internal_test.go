// An in-package test file joins the package's test variant, which is reported at the same first file as the package.
package utils

import "testing"

func TestParse(test *testing.T) {
	test.Parallel()
	if Parse("text") != "text" {
		test.Fatal("Parse changed its text")
	}
}
