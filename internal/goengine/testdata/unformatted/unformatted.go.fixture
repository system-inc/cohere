// Package unformatted is gofmt's to fix: a body indented with spaces, and two blank lines where gofmt keeps one,
// which moves every line below them up by one when the file is rewritten.
package unformatted

import "fmt"

// Triple is three times value.
func Triple(value int) int {
    return value*3
}


// Report prints a count with the wrong verb, which the directive withholds before and after gofmt moves it.
func Report() {
	//cohere:ignore printf the wrong verb is the fixture's point, and the directive must cover it on either side of gofmt
	fmt.Printf("%d\n", "three")
}
