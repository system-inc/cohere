// An external test package sits in the folder of the package it tests, which is reported on its own, so this file
// carries no finding.
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
