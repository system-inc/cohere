// An in-package test comes next by name, and the finding passes over it too.
package jsonlines

import "testing"

func TestLines(test *testing.T) {
	test.Parallel()
	if Lines != 1 {
		test.Fatal("Lines changed")
	}
}
