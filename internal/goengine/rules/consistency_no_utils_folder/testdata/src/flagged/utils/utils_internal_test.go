// want `^Folder name "utils" is not allowed\. Use "utilities" instead\.$`

// An in-package test file is a file of the folder like any other.
package utils

import "testing"

func TestParse(test *testing.T) {
	test.Parallel()
	if Parse("text") != "text" {
		test.Fatal("Parse changed its text")
	}
}
