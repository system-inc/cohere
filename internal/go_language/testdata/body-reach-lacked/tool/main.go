// Command tool has an in-package test and is imported by nothing, so an extra build's load leaves it out and checks
// its test variant in its place.
package main

// main does nothing with value.
func main() {
	_ = value()
}

// value is one.
func value() int {
	return 1
}
