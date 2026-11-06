// want `^Folder name "utils" is not allowed\. Use "utilities" instead\.$`

// An external test package sits in the folder of the package it tests, and its file is judged by that folder,
// though its import path ends in utils_test.
package utils_test

import (
	"testing"

	"flagged/utils"
)

func TestFormat(test *testing.T) {
	test.Parallel()
	if utils.Format("text") != "text" {
		test.Fatal("Format changed its text")
	}
}
