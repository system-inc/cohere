// Package bottom holds Double, which the test's edits keep the same API and positions while its body changes.
package bottom

// Double returns twice value. It is the file's last declaration, so an edit to its body moves no other position.
//
//go:noinline
func Double(value int) int {
	return value * 2
}
