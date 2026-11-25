// Package test_main_file holds a TestMain as go test runs one, a function in a test file, after a call to os.Exit at
// package level in a file the walk reads before it.
package test_main_file

import "os"

// exitAtPackageLevel calls os.Exit at package level, which the original counts for the TestMain of main_test.go.
var exitAtPackageLevel = func() int {
	if len(os.Args) > 100 {
		os.Exit(2)
	}
	return 0
}()
